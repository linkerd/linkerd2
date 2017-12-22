import _ from 'lodash';
import BarChart from './BarChart.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import HealthPane from './HealthPane.jsx';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import Metric from './Metric.jsx';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import StatPane from './StatPane.jsx';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { ApiHelpers, urlsForResource } from './util/ApiHelpers.js';
import { Col, Row } from 'antd';
import { emptyMetric, getPodsByDeployment, processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import './../../css/deployment.css';
import 'whatwg-fetch';

export default class Deployment extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
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
  }

  initialState(location) {
    let urlParams = new URLSearchParams(location.search);
    let deployment = urlParams.get("deploy");
    return {
      lastUpdated: 0,
      pollingInterval: 10000,
      metricsWindow: "10m",
      deploy: deployment,
      metrics:[],

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

    let urls = urlsForResource(this.props.pathPrefix, this.state.metricsWindow);

    let metricsUrl = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}` ;
    let deployMetricsUrl = `${metricsUrl}&timeseries=true&target_deploy=${this.state.deploy}`;
    let podRollupUrl = urls["pod"].url(this.state.deploy).rollup;
    let upstreamRollupUrl = urls["upstream_deployment"].url(this.state.deploy).rollup;
    let downstreamRollupUrl = urls["downstream_deployment"].url(this.state.deploy).rollup;

    let deployFetch = this.api.fetch(deployMetricsUrl);
    let podListFetch = this.api.fetchPods();
    let podRollupFetch = this.api.fetch(podRollupUrl);
    let upstreamFetch = this.api.fetch(upstreamRollupUrl);
    let downstreamFetch = this.api.fetch(downstreamRollupUrl);

    // expose serverPromise for testing
    this.serverPromise = Promise.all([deployFetch, podRollupFetch, upstreamFetch, downstreamFetch, podListFetch])
      .then(([deployMetrics, podRollup, upstreamRollup, downstreamRollup, podList]) => {
        let tsByDeploy = processTimeseriesMetrics(deployMetrics.metrics, "targetDeploy");
        let podMetrics = processRollupMetrics(podRollup.metrics, "targetPod");
        let upstreamMetrics = processRollupMetrics(upstreamRollup.metrics, "sourceDeploy");
        let downstreamMetrics = processRollupMetrics(downstreamRollup.metrics, "targetDeploy");

        let deploy = _.find(getPodsByDeployment(podList.pods), ["name", this.state.deploy]);
        let totalRequestRate = _.sumBy(podMetrics, "requestRate");
        _.each(podMetrics, datum => datum.totalRequests = totalRequestRate);

        this.setState({
          metrics: podMetrics,
          pods: deploy.pods,
          added: deploy.added,
          deployTs: _.get(tsByDeploy, this.state.deploy, {}),
          upstreamMetrics: upstreamMetrics,
          downstreamMetrics: downstreamMetrics,
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
      <HealthPane
        key="deploy-health-pane"
        entity={this.state.deploy}
        entityType="deployment"
        currentSr={currentSuccessRate}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        deploymentAdded={this.state.added} />,
      _.isEmpty(this.state.deployTs) ? null :
        <StatPane
          key="stat-pane"
          lastUpdated={this.state.lastUpdated}
          timeseries={this.state.deployTs} />,
      this.renderMidsection(),
      <UpstreamDownstream
        key="deploy-upstream-downstream"
        resource="deployment"
        entity={this.state.deploy}
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        metricsWindow={this.state.metricsWindow}
        pathPrefix={this.props.pathPrefix} />
    ];
  }

  renderMidsection() {
    let podTableData = this.state.metrics;
    if (_.isEmpty(this.state.metrics)) {
      podTableData = _.map(this.state.pods, po => emptyMetric(po.name));
    }

    return (
      <Row gutter={rowGutter} key="deployment-midsection">
        <Col span={16}>
          <div className="pod-summary">
            <div className="border-container border-neutral subsection-header">
              <div className="border-container-content subsection-header">Pod summary</div>
            </div>
            {
              _.isEmpty(this.state.metrics) ? null :
                <div className="pod-distribution-chart">
                  <div className="bar-chart-title">
                    <div>Request load by pod</div>
                    <div className="bar-chart-tooltip" />
                  </div>
                  <BarChart
                    data={this.state.metrics}
                    lastUpdated={this.state.lastUpdated}
                    containerClassName="pod-distribution-chart" />
                </div>
            }
            <TabbedMetricsTable
              resource="pod"
              entity={this.state.deploy}
              metrics={podTableData}
              lastUpdated={this.state.lastUpdated}
              pathPrefix={this.props.pathPrefix}
              metricsWindow={this.state.metricsWindow} />
          </div>
        </Col>

        <Col span={8}>
          <div className="border-container border-neutral deployment-details">
            <div className="border-container-content">
              <div className=" subsection-header">Deployment details</div>
              <Metric title="Pods" value={_.size(podTableData)} />
              <Metric title="Upstream deployments" value={this.numUpstreams()} />
              <Metric title="Downstream deployments" value={this.numDownstreams()} />
            </div>
          </div>
        </Col>
      </Row>
    );
  }

  renderDeploymentTitle() {
    return (
      <div className="deployment-title">
        <h1>{this.state.deploy}</h1>
        {
          !this.state.added ? (
            <div className="unadded-message">
              <div className="status-badge unadded"><p>UNADDED</p></div>
              <div className="call-to-action">
                {incompleteMeshMessage(this.state.deploy)}
              </div>
            </div>
          ) : null
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
            <div className="page-header">
              <div className="subsection-header">Deployment detail</div>
              {this.renderDeploymentTitle()}
            </div>
            {this.renderSections()}
          </div>
        }
      </div>
    );
  }
}
