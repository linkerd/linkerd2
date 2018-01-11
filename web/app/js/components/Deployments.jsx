import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import DeploymentSummary from './DeploymentSummary.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import React from 'react';
import ScatterPlot from './ScatterPlot.jsx';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { ApiHelpers, urlsForResource } from './util/ApiHelpers.js';
import { Col, Row } from 'antd';
import { emptyMetric, getPodsByDeployment, processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import { metricToFormatter, rowGutter } from './util/Utils.js';
import './../../css/deployments.css';
import 'whatwg-fetch';

const maxTsToFetch = 15; // Beyond this, stop showing sparklines in table
const getP99 = d => _.get(d, ["latency", "P99", 0, "value"]);
let nodeStats = (description, node) => (
  <div>
    <div className="title">{description}:</div>
    <div>
      {node.name} ({metricToFormatter["LATENCY"](getP99(node))})
    </div>
  </div>
);

export default class Deployments extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.loadTimeseriesFromServer = this.loadTimeseriesFromServer.bind(this);

    this.state = {
      metricsWindow: "10m",
      pollingInterval: 10000, // TODO: poll based on metricsWindow size
      metrics: [],
      timeseriesByDeploy: {},
      lastUpdated: 0,
      limitSparklineData: false,
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
  }

  addDeploysWithNoMetrics(deploys, metrics) {
    // also display deployments which have not been added to the service mesh
    // (and therefore have no associated metrics)
    let newMetrics = [];
    let metricsByName = _.groupBy(metrics, 'name');
    _.each(deploys, data => {
      newMetrics.push(_.get(metricsByName, [data.name, 0], emptyMetric(data.name, data.added)));
    });
    return newMetrics;
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let rollupPath = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}`;

    let rollupRequest = this.api.fetch(rollupPath);
    let podsRequest = this.api.fetchPods(this.props.pathPrefix);

    // expose serverPromise for testing
    this.serverPromise = Promise.all([rollupRequest, podsRequest])
      .then(([rollup, p]) => {
        let poByDeploy = getPodsByDeployment(p.pods);
        let meshDeploys = processRollupMetrics(rollup.metrics, "targetDeploy");
        let combinedMetrics = this.addDeploysWithNoMetrics(poByDeploy, meshDeploys);

        this.loadTimeseriesFromServer(meshDeploys, combinedMetrics);
      })
      .catch(this.handleApiError);
  }

  loadTimeseriesFromServer(meshDeployMetrics, combinedMetrics) {
    // fetch only the timeseries for the 3 deployments we display at the top of the page
    let limitSparklineData = _.size(meshDeployMetrics) > maxTsToFetch;

    let resourceInfo = urlsForResource(this.props.pathPrefix, this.state.metricsWindow)["deployment"];
    let leastHealthyDeployments = this.getLeastHealthyDeployments(meshDeployMetrics);

    let tsPromises = _.map(leastHealthyDeployments, dep => {
      let tsPathForDeploy = resourceInfo.url(dep.name).ts;
      return this.api.fetch(tsPathForDeploy);
    });

    Promise.all(tsPromises)
      .then(tsMetrics => {
        let leastHealthyTs = _.reduce(tsMetrics, (mem, ea) => {
          mem = mem.concat(ea.metrics);
          return mem;
        }, []);
        let tsByDeploy = processTimeseriesMetrics(leastHealthyTs, resourceInfo.groupBy);
        this.setState({
          timeseriesByDeploy: tsByDeploy,
          lastUpdated: Date.now(),
          metrics: combinedMetrics,
          limitSparklineData: limitSparklineData,
          loaded: true,
          pendingRequests: false,
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

  getLeastHealthyDeployments(deployMetrics, limit = 3) {
    return _(deployMetrics)
      .filter('added')
      .sortBy('successRate')
      .take(limit)
      .value();
  }

  renderPageContents() {
    let leastHealthyDeployments = this.getLeastHealthyDeployments(this.state.metrics);
    let slowestNode = _.maxBy(this.state.metrics, getP99);
    let fastestNode = _.minBy(this.state.metrics, getP99);

    let scatterplotData = _.reduce(this.state.metrics, (mem, datum) => {
      if (!_.isNil(datum.successRate) && !_.isNil(datum.latency)) {
        mem.push(datum);
      }
      return mem;
    }, []);

    return (
      <div className="clearfix">
        {_.isEmpty(leastHealthyDeployments) ? null : <div className="subsection-header">Least-healthy deployments</div>}
        <Row gutter={rowGutter}>
          {
            _.map(leastHealthyDeployments, deployment => {
              return (<Col span={8} key={`col-${deployment.name}`}>
                <DeploymentSummary
                  key={deployment.name}
                  lastUpdated={this.state.lastUpdated}
                  data={deployment}
                  requestTs={_.get(this.state.timeseriesByDeploy, [deployment.name, "REQUEST_RATE"], [])}
                  pathPrefix={this.props.pathPrefix} />
              </Col>);
            })
          }
        </Row>
        <Row gutter={rowGutter}>
          { _.isEmpty(scatterplotData) ? null :
            <div className="deployments-scatterplot">
              <div className="scatterplot-info">
                <div className="subsection-header">Success rate vs p99 latency</div>
              </div>
              <Row gutter={rowGutter}>
                <Col span={8}>
                  <div className="scatterplot-display">
                    <div className="extremal-latencies">
                      { !fastestNode ? null : nodeStats("Least latency", fastestNode) }
                      { !slowestNode ? null : nodeStats("Most latency", slowestNode) }
                    </div>
                  </div>
                </Col>
                <Col span={16}><div className="scatterplot-chart">
                  <ScatterPlot
                    data={scatterplotData}
                    lastUpdated={this.state.lastUpdated}
                    containerClassName="scatterplot-chart" />
                </div></Col>
              </Row>
            </div>
          }
        </Row>
        <div className="deployments-list">
          <TabbedMetricsTable
            resource="deployment"
            lastUpdated={this.state.lastUpdated}
            metrics={this.state.metrics}
            hideSparklines={this.state.limitSparklineData}
            metricsWindow={this.state.metricsWindow}
            pathPrefix={this.props.pathPrefix} />
        </div>
      </div>
    );
  }

  render() {
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner />  :
          <div>
            <div className="page-header">
              <h1>All deployments</h1>
            </div>
            { _.isEmpty(this.state.metrics) ?
              <CallToAction numDeployments={_.size(this.state.metrics)} /> :
              this.renderPageContents()
            }
          </div>
        }
      </div>);
  }
}
