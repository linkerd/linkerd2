import _ from 'lodash';
import BarChart from './BarChart.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import HealthPane from './HealthPane.jsx';
import Metric from './Metric.jsx';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import StatPane from './StatPane.jsx';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { Col, Row } from 'antd';
import { processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import './../../css/deployment.css';
import 'whatwg-fetch';

export default class Deployment extends React.Component {
  constructor(props) {
    super(props);
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
      timeseriesByPod: {},
      upstreamMetrics: [],
      upstreamTsByDeploy: {},
      downstreamMetrics: [],
      downstreamTsByDeploy: {},
      pendingRequests: false,
      loaded: false
    };
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let metricsUrl = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}` ;
    let deployMetricsUrl = `${metricsUrl}&timeseries=true&target_deploy=${this.state.deploy}`;
    let podRollupUrl = `${metricsUrl}&aggregation=target_pod&target_deploy=${this.state.deploy}`;
    let podTimeseriesUrl = `${podRollupUrl}&timeseries=true`;
    let upstreamRollupUrl = `${metricsUrl}&aggregation=source_deploy&target_deploy=${this.state.deploy}`;
    let upstreamTimeseriesUrl = `${upstreamRollupUrl}&timeseries=true`;
    let downstreamRollupUrl = `${metricsUrl}&aggregation=target_deploy&source_deploy=${this.state.deploy}`;
    let downstreamTimeseriesUrl = `${downstreamRollupUrl}&timeseries=true`;

    let deployFetch = fetch(deployMetricsUrl).then(r => r.json());
    let podFetch = fetch(podRollupUrl).then(r => r.json());
    let podTsFetch = fetch(podTimeseriesUrl).then(r => r.json());
    let upstreamFetch = fetch(upstreamRollupUrl).then(r => r.json());
    let upstreamTsFetch = fetch(upstreamTimeseriesUrl).then(r => r.json());
    let downstreamFetch = fetch(downstreamRollupUrl).then(r => r.json());
    let downstreamTsFetch = fetch(downstreamTimeseriesUrl).then(r => r.json());

    Promise.all([deployFetch, podFetch, podTsFetch, upstreamFetch, upstreamTsFetch, downstreamFetch, downstreamTsFetch])
      .then(([deployMetrics, podRollup, podTimeseries, upstreamRollup, upstreamTimeseries, downstreamRollup, downstreamTimeseries]) => {
        let tsByDeploy = processTimeseriesMetrics(deployMetrics.metrics, "targetDeploy");
        let podMetrics = processRollupMetrics(podRollup.metrics, "targetPod");
        let podTs = processTimeseriesMetrics(podTimeseries.metrics, "targetPod");

        let upstreamMetrics = processRollupMetrics(upstreamRollup.metrics, "sourceDeploy");
        let upstreamTsByDeploy = processTimeseriesMetrics(upstreamTimeseries.metrics, "sourceDeploy");
        let downstreamMetrics = processRollupMetrics(downstreamRollup.metrics, "targetDeploy");
        let downstreamTsByDeploy = processTimeseriesMetrics(downstreamTimeseries.metrics, "targetDeploy");

        let totalRequestRate = _.sumBy(podMetrics, "requestRate");
        _.each(podMetrics, datum => datum.totalRequests = totalRequestRate);

        this.setState({
          metrics: podMetrics,
          timeseriesByPod: podTs,
          deployTs: _.get(tsByDeploy, this.state.deploy, {}),
          upstreamMetrics: upstreamMetrics,
          upstreamTsByDeploy: upstreamTsByDeploy,
          downstreamMetrics: downstreamMetrics,
          downstreamTsByDeploy: downstreamTsByDeploy,
          lastUpdated: Date.now(),
          pendingRequests: false,
          loaded: true
        });
      }).catch(() => {
        this.setState({ pendingRequests: false });
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
        downstreamMetrics={this.state.downstreamMetrics} />,
      <StatPane
        key="stat-pane"
        lastUpdated={this.state.lastUpdated}
        timeseries={this.state.deployTs} />,
      this.renderMidsection(),
      <UpstreamDownstream
        key="deploy-upstream-downstream"
        entity="deployment"
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        upstreamTsByEntity={this.state.upstreamTsByDeploy}
        downstreamMetrics={this.state.downstreamMetrics}
        downstreamTsByEntity={this.state.downstreamTsByDeploy}
        pathPrefix={this.props.pathPrefix} />
    ];
  }

  renderMidsection() {
    return (
      <Row gutter={rowGutter} key="deployment-midsection">
        <Col span={16}>
          <div className="pod-summary">
            <div className="border-container border-neutral subsection-header">
              <div className="border-container-content subsection-header">Pod summary</div>
            </div>
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

            <TabbedMetricsTable
              resource="pod"
              lastUpdated={this.state.lastUpdated}
              metrics={this.state.metrics}
              timeseries={this.state.timeseriesByPod}
              pathPrefix={this.props.pathPrefix} />
          </div>
        </Col>

        <Col span={8}>
          <div className="border-container border-neutral deployment-details">
            <div className="border-container-content">
              <div className=" subsection-header">Deployment details</div>
              <Metric title="Pods" value={_.size(this.state.metrics)} />
              <Metric title="Upstream deployments" value={this.numUpstreams()} />
              <Metric title="Downstream deployments" value={this.numDownstreams()} />
            </div>
          </div>
        </Col>
      </Row>
    );
  }

  renderContent() {
    if (_.isEmpty(this.state.metrics)) {
      return <div>No data</div>;
    } else {
      return this.renderSections();
    }
  }

  render() {
    if (!this.state.loaded) {
      return <ConduitSpinner />;
    } else return (
      <div className="page-content deployment-detail">
        <div className="page-header">
          <div className="subsection-header">Deployment detail</div>
          <h1>{this.state.deploy}</h1>
        </div>
        {this.renderContent()}
      </div>
    );
  }
}
