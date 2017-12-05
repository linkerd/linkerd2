import React from 'react';
import _ from 'lodash';
import { Table, Row, Col, Progress } from 'antd';
import styles from './../../css/deployment.css';
import BarChart from './BarChart.jsx';
import Metric from './Metric.jsx';
import HealthPane from './HealthPane.jsx';
import StatPane from './StatPane.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { rowGutter, metricToFormatter } from './util/Utils.js';
import { processMetrics, processTsWithLatencyBreakdown } from './util/MetricUtils.js';
import ConduitSpinner from "./ConduitSpinner.jsx";

const podColumns = [
  {
    title: "Pod name",
    dataIndex: "podName",
    key: "podName"
  },
  {
    title: "Request rate",
    dataIndex: "requestRate",
    key: "requestRate",
    className: "numeric"
  }
];

export default class Deployment extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.initialState(this.props.location);
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
      upstreamMetrics: [],
      downstreamMetrics: [],
      summaryMetrics: {},
      pendingRequests: false,
      loaded: false
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  componentWillReceiveProps(nextProps) {
    window.scrollTo(0, 0);
    this.setState(this.initialState(nextProps.location), () => {
      this.loadFromServer();
    });
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
    let upstreamRollupUrl = `${metricsUrl}&aggregation=source_deploy&target_deploy=${this.state.deploy}`
    let upstreamTimeseriesUrl = `${upstreamRollupUrl}&timeseries=true`;
    let downstreamRollupUrl = `${metricsUrl}&aggregation=target_deploy&source_deploy=${this.state.deploy}`
    let downstreamTimeseriesUrl = `${downstreamRollupUrl}&timeseries=true`;

    let deployFetch = fetch(deployMetricsUrl).then(r => r.json());
    let podFetch = fetch(podRollupUrl).then(r => r.json());
    let podTsFetch = fetch(podTimeseriesUrl).then(r => r.json());
    let upstreamFetch = fetch(upstreamRollupUrl).then(r => r.json());
    let upstreamTsFetch = fetch(upstreamTimeseriesUrl).then(r => r.json());
    let downstreamFetch = fetch(downstreamRollupUrl).then(r => r.json());
    let downstreamTsFetch = fetch(downstreamTimeseriesUrl).then(r => r.json());

    Promise.all([deployFetch, podFetch, podTsFetch, upstreamFetch, upstreamTsFetch, downstreamFetch, downstreamTsFetch])
      .then(([deployMetrics, podRollup, podTimeseries, upstreamRollup, upsreamTimeseries, downstreamRollup, downstreamTimeseries]) => {
        let deployTimeseries = processTsWithLatencyBreakdown(deployMetrics.metrics);
        let podMetrics = _.compact(processMetrics(podRollup.metrics, podTimeseries.metrics, "targetPod"));
        let upstreamMetrics = _.compact(processMetrics(upstreamRollup.metrics, upsreamTimeseries.metrics, "sourceDeploy"));
        let downstreamMetrics = _.compact(processMetrics(downstreamRollup.metrics, downstreamTimeseries.metrics, "targetDeploy"));

        let totalRequestRate = _.sumBy(podMetrics, "rollup.requestRate");
        _.each(podMetrics, datum => datum.rollup.totalRequests = totalRequestRate);

        this.setState({
          metrics: podMetrics,
          upstreamMetrics: upstreamMetrics,
          downstreamMetrics: downstreamMetrics,
          summaryMetrics: deployTimeseries,
          lastUpdated: Date.now(),
          pendingRequests: false,
          loaded: true
        });
      }).catch(err => {
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
    return [
      <HealthPane
        key="deploy-health-pane"
        entity={this.state.deploy}
        entityType="deployment"
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
      />,
      <StatPane
        key="stat-pane"
        lastUpdated={this.state.lastUpdated}
        summaryMetrics={this.state.summaryMetrics}
      />,
      this.renderMidsection(),
      <UpstreamDownstream
        key="deploy-upstream-downstream"
        entity="deployment"
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        pathPrefix={this.props.pathPrefix}
      />
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
                <div className="bar-chart-tooltip"></div>
              </div>
              <BarChart
                data={this.state.metrics}
                lastUpdated={this.state.lastUpdated}
                containerClassName="pod-distribution-chart"
              />
            </div>

            <TabbedMetricsTable
              resource="pod"
              lastUpdated={this.state.lastUpdated}
              metrics={this.state.metrics}
              pathPrefix={this.props.pathPrefix}
            />
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
      return <ConduitSpinner />
    } else return (
      <div className="page-content deployment-detail">
        <div className="page-header">
          <div className="subsection-header">Deployment detail</div>
          <h1>{this.state.deploy}</h1>
          {this.renderContent()}
        </div>
      </div>
    );
  }
}
