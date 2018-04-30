import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import { processRollupMetrics } from './util/MetricUtils.js';
import React from 'react';
import './../../css/list.css';
import 'whatwg-fetch';

export default class Namespaces extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(this.props.location);
  }

  getInitialState(location) {
    location = location || {};
    let urlParams = new URLSearchParams(location.search);
    let ns = urlParams.get("ns") || "default";

    return {
      ns: ns,
      pollingInterval: 2000,
      metrics: {},
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillReceiveProps() {
    // React won't unmount this component when switching resource pages so we need to clear state
    this.api.cancelCurrentRequests();
    this.setState(this.getInitialState(this.props.location));
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests(
      _.map(["deployment", "replication_controller", "pod"], resource =>
        this.api.fetchMetrics(this.api.urlsForResource[resource].url(this.state.ns).rollup))
    );

    Promise.all(this.api.getCurrentPromises())
      .then(([deployRollup, rcRollup, podRollup]) => {
        let includeConduitStats = this.state.ns === this.props.controllerNamespace; // allow us to get stats on the conduit ns
        let deploys = processRollupMetrics(deployRollup, this.props.controllerNamespace, includeConduitStats);
        let rcs = processRollupMetrics(rcRollup, this.props.controllerNamespace, includeConduitStats);
        let pods = processRollupMetrics(podRollup, this.props.controllerNamespace, includeConduitStats);

        this.setState({
          metrics: {
            deploy: deploys,
            rc: rcs,
            pod: pods
          },
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

  renderResourceSection(friendlyTitle, metrics) {
    if (_.isEmpty(metrics)) {
      return null;
    }
    return (
      <div className="page-section">
        <h1>{friendlyTitle}</h1>
        <MetricsTable
          resource={friendlyTitle}
          metrics={metrics}
          api={this.api} />
      </div>
    );
  }

  render() {
    let noMetrics = _.isEmpty(this.state.metrics.deploy) && _.isEmpty(this.state.metrics.rc);

    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner />  :
          <div>
            <PageHeader header={"Namespace: " + this.state.ns} api={this.api} />
            { noMetrics ? <CallToAction /> : null}
            {this.renderResourceSection("Deployments", this.state.metrics.deploy)}
            {this.renderResourceSection("Replication Controllers", this.state.metrics.rc)}
            {this.renderResourceSection("Pods", this.state.metrics.pod)}
          </div>
        }
      </div>);
  }
}
