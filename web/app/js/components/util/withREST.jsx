import _ from 'lodash';
import React from 'react';

/**
 * Provides components with data fetched via. polling a list of REST URLs.
 * @constructor
 * @param {React.Component} WrappedComponent - Component to add functionality to.
 * @param {List[string]} requestURLs - List of URLs to poll.
 * @param {List[string]} resetProps - Props that on change cause a state reset.
 */
const withREST = (WrappedComponent, componentPromises, resetProps = []) => {
  return class extends React.Component {
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
        resetProps,
        prop => _.get(newProps, prop) !== _.get(this.props, prop),
      );

      if (_.isEmpty(changed)) return;

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
      if (e.isCanceled) return;

      this.setState({
        pendingRequests: false,
        error: `Error getting data from server: ${e.message}`
      });
    }

    render() {
      return (
        <WrappedComponent
          {..._.pick(this.state, ['data', 'error', 'loading'])}
          {...this.props} />
      );
    }
  };
};

export default withREST;
