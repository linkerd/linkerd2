import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import React from 'react';
import { getPodsByResource, processRollupMetrics } from './util/MetricUtils.js';
import './../../css/list.css';
import 'whatwg-fetch';

export default class ReplicationControllersList extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      pollingInterval: 2000, // TODO: poll based on metricsWindow size
      metrics: [],
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  filterResources(resourceList, metrics) {
    let resourcesByName = _.keyBy(resourceList, 'name');
    return _.compact(_.map(metrics, metric => {
      if (_.has(resourcesByName, metric.name)) {
        metric.added = resourcesByName[metric.name].added;
      }
      return metric;
    }));
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource["replication_controller"].url().rollup),
      this.api.fetchPods()
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([rollup, p]) => {
        let poByResource = getPodsByResource(p.pods, "replicationController");
        let meshResources = processRollupMetrics(rollup);
        let combinedMetrics = this.filterResources(poByResource, meshResources);

        this.setState({
          metrics: combinedMetrics,
          loaded: true,
          pendingRequests: false,
          error: ''
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError(e) {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: `Error getting data from server: ${e.message}`
    });
  }

  render() {
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner />  :
          <div>
            <PageHeader header="Replication Controllers" api={this.api} />
            { _.isEmpty(this.state.metrics) ?
              <CallToAction /> :
              <MetricsTable
                resource="Replication Controller"
                metrics={this.state.metrics}
                api={this.api} />
            }
          </div>
        }
      </div>);
  }
}
