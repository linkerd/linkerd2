import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import _isEmpty from 'lodash/isEmpty';
import { processGatewayResults } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

class Gateways extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = {
      pollingInterval: 2000,
      metrics: {},
      pendingRequests: false,
      loaded: false,
      error: null,
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentDidUpdate(prevProps) {
    const { isPageVisible } = this.props;

    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  startServerPolling() {
    const { pollingInterval } = this.state;

    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({ pendingRequests: false });
  }

  loadFromServer() {
    const { pendingRequests } = this.state;
    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([this.api.fetchGateways()]);

    Promise.all(this.api.getCurrentPromises())
      .then(([gatewayMetrics]) => {
        const metrics = processGatewayResults(gatewayMetrics);
        this.setState({
          metrics,
          loaded: true,
          pendingRequests: false,
          error: null,
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e,
    });
  };

  render() {
    const { metrics, loaded, error } = this.state;
    const noMetrics = _isEmpty(metrics);
    return (
      <div className="page-content">
        {!error ? null : <ErrorBanner message={error} />}
        {!loaded ? (
          <Spinner />
        ) : (
          <div>
            {noMetrics ? <div>No resources detected.</div> : null}
            {noMetrics ? null : (
              <div className="page-section">
                <MetricsTable
                  title="Gateways"
                  resource="gateway"
                  metrics={metrics} />
              </div>
            )}
          </div>
        )}
      </div>
    );
  }
}

Gateways.propTypes = {
  api: PropTypes.shape({
    cancelCurrentRequests: PropTypes.func.isRequired,
    fetchGateways: PropTypes.func.isRequired,
    getCurrentPromises: PropTypes.func.isRequired,
    setCurrentRequests: PropTypes.func.isRequired,
  }).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
};

export default withPageVisibility(withContext(Gateways));
