import _ from 'lodash';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import MetricsSummary from './MetricsSummary.jsx';
import PageHeader from './PageHeader.jsx';
import React from 'react';
import ResourceHealthOverview from './ResourceHealthOverview.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { getPodsByDeployment, processRollupMetrics } from './util/MetricUtils.js';
import './../../css/deployment.css';
import 'whatwg-fetch';

export default class DeploymentDetail extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.initialState(this.props.location);
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillReceiveProps(nextProps) {
    window.scrollTo(0, 0);
    this.setState(this.initialState(nextProps.location), () => {
      this.loadFromServer();
    });
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  initialState(location) {
    let urlParams = new URLSearchParams(location.search);
    let deployment = urlParams.get("deploy");
    return {
      lastUpdated: 0,
      pollingInterval: 2000,
      deploy: deployment,
      pods: [],
      upstreamMetrics: [],
      downstreamMetrics: [],
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let urls = this.api.urlsForResource;

    let deployMetricsUrl = urls["deployment"].url(this.state.deploy).rollup;
    let upstreamRollupUrl = urls["upstream_deployment"].url(this.state.deploy).rollup;
    let downstreamRollupUrl = urls["downstream_deployment"].url(this.state.deploy).rollup;

    this.api.setCurrentRequests([
      this.api.fetchMetrics(deployMetricsUrl),
      this.api.fetchMetrics(upstreamRollupUrl),
      this.api.fetchMetrics(downstreamRollupUrl),
      this.api.fetchPods()
    ]);

    // expose serverPromise for testing
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([deployMetrics, upstreamRollup, downstreamRollup, podList]) => {
        let deployRollup = processRollupMetrics(deployMetrics.metrics, "targetDeploy");
        let upstreamMetrics = processRollupMetrics(upstreamRollup.metrics, "sourceDeploy");
        let downstreamMetrics = processRollupMetrics(downstreamRollup.metrics, "targetDeploy");

        let deploy = _.find(getPodsByDeployment(podList.pods), ["name", this.state.deploy]);

        this.setState({
          added: deploy.added,
          pods: deploy.pods,
          deployMetrics: _.get(deployRollup, 0, {}),
          deployTs: {},
          upstreamMetrics: upstreamMetrics,
          downstreamMetrics: downstreamMetrics,
          lastUpdated: Date.now(),
          pendingRequests: false,
          loaded: true,
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

  numUpstreams() {
    return _.size(this.state.upstreamMetrics);
  }

  numDownstreams() {
    return _.size(this.state.downstreamMetrics);
  }

  renderSections() {
    let srTs = _.get(this.state.deployTs, "SUCCESS_RATE", []);
    let currentSuccessRate = _.get(_.last(srTs), "value");

    return [
      <MetricsSummary
        key="metrics-summary"
        metrics={this.state.deployMetrics} />,
      <ResourceHealthOverview
        key="deploy-health-pane"
        resourceName={this.state.deploy}
        resourceType="deployment"
        currentSr={currentSuccessRate}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        deploymentAdded={this.state.added} />,
      <UpstreamDownstream
        key="deploy-upstream-downstream"
        resourceType="deployment"
        resourceName={this.state.deploy}
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        api={this.api} />,
    ];
  }

  renderDeploymentTitle() {
    return (
      <div className="deployment-title">
        <h1>{this.state.deploy}</h1>
        {
          !this.state.added ? (
            <p className="status-badge unadded">UNADDED</p>
          ) : (
            <span>&nbsp;<GrafanaLink name={this.state.deploy} size={32} conduitLink={this.api.ConduitLink} /></span>
          )
        }
      </div>
    );
  }

  render() {
    return (
      <div className="page-content deployment-detail">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner /> :
          <div>
            <PageHeader
              subHeaderTitle="Deployment detail"
              subHeader={this.renderDeploymentTitle()}
              subMessage={!this.state.added ? incompleteMeshMessage(this.state.deploy) : null}
              api={this.api} />

            {this.renderSections()}
          </div>
        }
      </div>
    );
  }
}
