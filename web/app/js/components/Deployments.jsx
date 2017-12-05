import React from 'react';
import _ from 'lodash';
import 'whatwg-fetch';
import { Row, Col } from 'antd';
import ConduitSpinner from "./ConduitSpinner.jsx";
import DeploymentSummary from './DeploymentSummary.jsx';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { rowGutter } from './util/Utils.js';
import { processMetrics, emptyMetric } from './util/MetricUtils.js';
import CallToAction from './CallToAction.jsx';
import styles from './../../css/deployments.css';

export default class Deployments extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = {
      metricsWindow: "10m",
      pollingInterval: 10000, // TODO: poll based on metricsWindow size
      metrics: [],
      lastUpdated: 0,
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

  processDeploys(pods) {
    let ns = this.props.namespace + '/';
    return _(pods)
      .reject(p => _.isEmpty(p.deployment) || _.startsWith(p.deployment, ns))
      .groupBy("deployment")
      .map((componentPods, name) => {
          return {name: name, added: _.every(componentPods, 'added')}
      })
      .groupBy("name")
      .value();
  }

  addOtherDeployments(deploys, metrics) {
    let groupedMetrics = _.groupBy(metrics, 'name');
    _.each(deploys, (data, name) => {
      if (!groupedMetrics[name]) {
        metrics.push(emptyMetric(name))
      }
    });
    return metrics;
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let rollupPath = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}`
    let timeseriesPath = `${rollupPath}&timeseries=true`
    let podPath = `${this.props.pathPrefix}/api/pods`
    let rollupRequest = fetch(rollupPath).then(r => r.json());
    let timeseriesRequest = fetch(timeseriesPath).then(r => r.json());
    let podRequest = fetch(podPath).then(r => r.json());

    Promise.all([rollupRequest, timeseriesRequest, podRequest])
      .then(([metrics, ts, p]) => {

        let po = this.processDeploys(p.pods)
        let m = _.compact(processMetrics(metrics.metrics, ts.metrics, "targetDeploy"));
        let newMetrics = this.addOtherDeployments(po, m);

        this.setState({
          metrics: newMetrics,
          lastUpdated: Date.now(),
          pendingRequests: false,
          loaded: true
        });
      }).catch(() => {
        this.setState({ pendingRequests: false });
      });
  }

  renderPageContents() {
    let leastHealthyDeployments = _(this.state.metrics)
      .reject(m => m.added === false)
      .sortBy(m => m.rollup.successRate)
      .take(3)
      .value();

    return (
      <div className="clearfix">
        <div className="subsection-header">Least-healthy deployments</div>
        { _.isEmpty(this.state.metrics) ? <div className="no-data-msg">No data</div> : null }
        <Row gutter={rowGutter}>
        {
          _.map(leastHealthyDeployments, deployment => {
            return <Col span={8} key={`col-${deployment.name}`}>
              <DeploymentSummary
                key={deployment.name}
                lastUpdated={this.state.lastUpdated}
                data={deployment}
                pathPrefix={this.props.pathPrefix}
              />
            </Col>
          })
        }
        </Row>
        <div className="deployments-list">
          <TabbedMetricsTable
            resource="deployment"
            lastUpdated={this.state.lastUpdated}
            metrics={this.state.metrics}
            pathPrefix={this.props.pathPrefix}
          />
        </div>
      </div>
    );
  }


  render() {
    if (!this.state.loaded) {
      return <ConduitSpinner />
    } else return (
      <div className="page-content">
        <div className="page-header">
          <h1>All deployments</h1>
          {_.isEmpty(this.state.metrics) ?
            <CallToAction numDeployments={_.size(this.state.metrics)} /> :
            null
          }
        </div>
        {!_.isEmpty(this.state.metrics) ? this.renderPageContents() : null}
      </div>
    );
  }
}
