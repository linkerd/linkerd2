import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './AppContext.jsx';

/**
 * Provides components with data fetched via. polling a list of REST URLs.
 * @constructor
 * @param {React.Component} WrappedComponent - Component to add functionality to.
 * @param {List[string]} requestURLs - List of URLs to poll.
 * @param {List[string]} options - Options for withREST
 */
const withREST = (WrappedComponent, componentPromises, options={}) => {
  const localOptions = _.merge({}, {
    resetProps: [],
    poll: true,
  }, options);

  class RESTWrapper extends React.Component {
    static propTypes = {
      api: PropTypes.shape({
        cancelCurrentRequests: PropTypes.func.isRequired,
        getCurrentPromises: PropTypes.func.isRequired,
        setCurrentRequests: PropTypes.func.isRequired,
      }).isRequired,
    }

    constructor(props) {
      super(props);
      this.api = this.props.api;

      this.state = this.getInitialState(this.props);
    }

    getInitialState = () => ({
      pollingInterval: 2000, // TODO: poll based on metricsWindow size
      data: [],
      pendingRequests: false,
      loading: true,
      error: ''
    });

    componentDidMount() {
      this.startServerPolling(this.props);
    }

    componentWillReceiveProps(newProps) {
      const changed = _.filter(
        localOptions.resetProps,
        prop => _.get(newProps, prop) !== _.get(this.props, prop),
      );

      if (_.isEmpty(changed)) { return; }

      // React won't unmount this component when switching resource pages so we need to clear state
      this.stopServerPolling();
      this.setState(this.getInitialState());
      this.startServerPolling(newProps);
    }

    componentWillUnmount() {
      this.stopServerPolling();
    }

    startServerPolling = props => {
      this.loadFromServer(props);
      if (!localOptions.poll) {
        return;
      }
      this.timerId = window.setInterval(
        this.loadFromServer, this.state.pollingInterval, props);
    }

    stopServerPolling = () => {
      window.clearInterval(this.timerId);
      this.api.cancelCurrentRequests();
    }

    loadFromServer = props => {
      if (this.state.pendingRequests) {
        return; // don't make more requests if the ones we sent haven't completed
      }

      this.setState({ pendingRequests: true });

      this.api.setCurrentRequests(componentPromises(props));

      Promise.all(this.api.getCurrentPromises())
        .then(data => {
          this.setState({
            data: data,
            loading: false,
            pendingRequests: false,
            error: '',
          });
        })
        .catch(this.handleApiError);
    }

    handleApiError = e => {
      if (e.isCanceled) { return; }

      this.setState({
        pendingRequests: false,
        error: `Error getting data from server: ${e.message}`
      });
    }

    render() {
      return (
        <WrappedComponent
          data={this.state.data}
          error={this.state.error}
          loading={this.state.loading}
          {...this.props} />
      );
    }
  }

  return withContext(RESTWrapper);
};

export default withREST;
