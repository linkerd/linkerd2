import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import PageHeader from './PageHeader.jsx';
import { processRollupMetrics } from './util/MetricUtils.js';
import React from 'react';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import 'whatwg-fetch';

export default class Paths extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
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

    let urls = this.api.urlsForResource;

    this.api.fetchMetrics(urls["path"].url().rollup).then(r => {
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
            <PageHeader header="Paths" api={this.api} />

            <TabbedMetricsTable
              resource="path"
              metrics={this.state.metrics}
              lastUpdated={this.state.lastUpdated}
              api={this.api}
              sortable={true}
              hideSparklines={true} />
          </div>
        }
      </div>
    );
  }
}
