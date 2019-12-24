import { handlePageVisibility, withPageVisibility } from './PageVisibility.jsx';
import PropTypes from 'prop-types';
import React from 'react';
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
    constructor(props) {
      super(props);
      this.api = props.api;

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
      const { isPageVisible } = this.props;
      handlePageVisibility({
        prevVisibilityState: prevProps.isPageVisible,
        currentVisibilityState: isPageVisible,
        onVisible: () => this.startServerPolling(this.props),
        onHidden: () => this.stopServerPolling(),
      });

      const changed = localOptions.resetProps.filter(
        prop => _get(prevProps, prop) !== _get(this.props, prop)
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
      const { pollingInterval } = this.state;
      this.loadFromServer(props);
      if (localOptions.poll) {
        this.timerId = window.setInterval(
          this.loadFromServer, pollingInterval, props
        );
      }
    }

    stopServerPolling = () => {
      this.api.cancelCurrentRequests();
      this.setState({ pendingRequests: false });
      if (localOptions.poll) {
        window.clearInterval(this.timerId);
      }
    }

    loadFromServer = props => {
      const { pendingRequests } = this.state;

      if (pendingRequests) {
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
      const { data, error, loading } = this.state;

      return (
        <WrappedComponent
          data={data}
          error={error}
          loading={loading}
          {...this.props} />
      );
    }
  }

  RESTWrapper.propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
    }).isRequired,
    isPageVisible: PropTypes.bool.isRequired,
  };

  return withPageVisibility(withContext(RESTWrapper));
};

export default withREST;
