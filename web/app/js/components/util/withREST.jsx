import PropTypes from 'prop-types';
import React from 'react';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _merge from 'lodash/merge';
import { withContext } from './AppContext.jsx';

/**
 * Provides components with data fetched via. polling a list of REST URLs.
 * @constructor
 * @param {React.Component} WrappedComponent - Component to add functionality to.
 * @param {List[string]} requestURLs - List of URLs to poll.
 * @param {List[string]} options - Options for withREST
 */
const withREST = (WrappedComponent, componentPromises, options={}) => {
  const localOptions = _merge({}, {
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
      error: null
    });

    componentDidMount() {
      this.startServerPolling(this.props);
    }

    componentDidUpdate(prevProps) {
      const changed = _filter(
        localOptions.resetProps,
        prop => _get(prevProps, prop) !== _get(this.props, prop),
      );

      if (_isEmpty(changed)) { return; }

      // React won't unmount this component when switching resource pages so we need to clear state
      this.stopServerPolling();
      this.setState(this.getInitialState());
      this.startServerPolling(this.props);
    }

    componentWillUnmount() {
      this.stopServerPolling();
    }

    startServerPolling = props => {
      this.loadFromServer(props);
      if (localOptions.poll) {
        this.timerId = window.setInterval(
          this.loadFromServer, this.state.pollingInterval, props);
      }
    }

    stopServerPolling = () => {
      this.api.cancelCurrentRequests();
      if (localOptions.poll) {
        window.clearInterval(this.timerId);
      }
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
            error: null,
          });
        })
        .catch(this.handleApiError);
    }

    handleApiError = e => {
      if (e.isCanceled) { return; }

      this.setState({
        pendingRequests: false,
        error: e
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
