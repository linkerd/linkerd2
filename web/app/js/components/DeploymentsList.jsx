import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import React from 'react';
import { getPodsByDeployment, processRollupMetrics } from './util/MetricUtils.js';
import './../../css/deployments.css';
import 'whatwg-fetch';

export default class DeploymentsList extends React.Component {
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

  filterDeploys(deploys, metrics) {
    let deploysByName = _.keyBy(deploys, 'name');
    return _.compact(_.map(metrics, metric => {
      if (_.has(deploysByName, metric.name)) {
        metric.added = deploysByName[metric.name].added;
        return metric;
      }
    }));
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource["deployment"].url().rollup),
      this.api.fetchPods()
    ]);

    // expose serverPromise for testing
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([rollup, p]) => {
        let poByDeploy = getPodsByDeployment(p.pods);
        let meshDeploys = processRollupMetrics(rollup);
        let combinedMetrics = this.filterDeploys(poByDeploy, meshDeploys);

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
            <PageHeader header="Deployments" api={this.api} />
            { _.isEmpty(this.state.metrics) ?
              <CallToAction numDeployments={_.size(this.state.metrics)} /> :
              <div className="deployments-list">
                <MetricsTable
                  resource="deployment"
                  metrics={this.state.metrics}
                  api={this.api} />
              </div>
            }
          </div>
        }
      </div>);
  }
}
