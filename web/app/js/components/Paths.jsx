import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import { processRollupMetrics } from './util/MetricUtils.js';
import React from 'react';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { ApiHelpers, urlsForResource } from './util/ApiHelpers.js';
import 'whatwg-fetch';

export default class Paths extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.initialState();
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  initialState() {
    return {
      lastUpdated: 0,
      pollingInterval: 10000,
      metricsWindow: "1m",
      metrics: [],
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return;
    }
    this.setState({ pendingRequests: true });

    let urls = urlsForResource(this.props.pathPrefix, this.state.metricsWindow);

    this.api.fetch(urls["path"].url().rollup).then(r => {
      let metrics = processRollupMetrics(r.metrics, "path");

      this.setState({
        metrics: metrics,
        lastUpdated: Date.now(),
        pendingRequests: false,
        loaded: true,
        error: ''
      });
    }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      pendingRequests: false,
      error: `Error getting data from server: ${e.message}`
    });
  }

  render() {
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner /> :
          <div>
            <div className="page-header">
              <h1>Paths</h1>
            </div>

            <TabbedMetricsTable
              resource="path"
              metrics={this.state.metrics}
              lastUpdated={this.state.lastUpdated}
              pathPrefix={this.props.pathPrefix}
              metricsWindow={this.state.metricsWindow}
              sortable={true}
              hideSparklines={true} />
          </div>
        }
      </div>
    );
  }
}
