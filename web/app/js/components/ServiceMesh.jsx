import React from 'react';
import _ from 'lodash';
import 'whatwg-fetch';
import { Table, Row, Col } from 'antd';
import DeploymentSummary from './DeploymentSummary.jsx';
import Metric from './Metric.jsx';
import StatusTable from './StatusTable.jsx';
import CallToAction from './CallToAction.jsx';
import { rowGutter } from './util/Utils.js';
import { processMetrics } from './util/MetricUtils.js';
import styles from './../../css/service-mesh.css';
import ConduitSpinner from "./ConduitSpinner.jsx";

const serviceMeshDetailsColumns = [
  {
    title: "Name",
    dataIndex: "name",
    key: "name"
  },
  {
    title: "Value",
    dataIndex: "value",
    key: "value",
    className: "numeric"
  }
];
const componentNames = {
  "prometheus":   "Prometheus",
  "destination":  "Controller destination",
  "proxy-api":    "Controller proxy-api",
  "public-api":   "Controller public-api",
  "tap":          "Controller tap",
  "telemetry":    "Controller telemetry",
  "web":          "Web UI"
}
const componentDeploys = {
  "prometheus":   "prometheus",
  "destination":  "controller",
  "proxy-api":    "controller",
  "public-api":   "controller",
  "tap":          "controller",
  "telemetry":    "controller",
  "web":          "web"
}
const componentsToGraph = ["proxy-api", "telemetry", "destination"]
const noData = {
  rollup: {},
  timeseries: { requestRate: [], successRate: [] }
}

export default class ServiceMesh extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      pollingInterval: 2000,
      metricsWindow: "10m",
      metrics: [],
      deploys: [],
      components: [],
      lastUpdated: 0,
      pendingRequests: false,
      loaded: false
    }
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let rollupPath = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}&aggregation=mesh`;
    let timeseriesPath = `${rollupPath}&timeseries=true`;
    let podsPath = `${this.props.pathPrefix}/api/pods`;
    let rollupRequest = fetch(rollupPath).then(r => r.json());
    let timeseriesRequest = fetch(timeseriesPath).then(r => r.json());
    let podsRequest = fetch(podsPath).then(r => r.json());

    Promise.all([rollupRequest, timeseriesRequest, podsRequest])
      .then(([metrics, ts, pods]) => {
        let m = _.compact(processMetrics(metrics.metrics, ts.metrics, "component"));
        let d = this.processDeploys(pods.pods);
        let c = this.processComponents(pods.pods);

        this.setState({
          metrics: m,
          deploys: d,
          components: c,
          lastUpdated: Date.now(),
          pendingRequests: false,
          loaded: true
        });
      }).catch(() => {
        this.setState({ pendingRequests: false });
      });
  }

  addedDeploymentCount() {
    return _.size(_.find(this.state.deploys, d => {
      return _.every(d.pods, ["value", "good"]);
    }));
  }

  unaddedDeploymentCount() {
    return _.size(this.state.deploys) - this.addedDeploymentCount();
  }

  proxyCount() {
    return _.sum(_.map(this.state.deploys, d => {
      return _.size(_.filter(d.pods, ["value", "good"]));
    }));
  }

  componentCount() {
    return _.size(this.state.components);
  }

  getServiceMeshDetails() {
    return [
      { key: 1, name: "Conduit version", value: this.props.releaseVersion },
      { key: 2, name: "Control plane components", value: this.componentCount() },
      { key: 3, name: "Added deployments", value: this.addedDeploymentCount() },
      { key: 4, name: "Unadded deployments", value: this.unaddedDeploymentCount() },
      { key: 5, name: "Data plane proxies", value: this.proxyCount() }
    ]
  }

  processDeploys(pods) {
    let ns = this.props.namespace + "/";
    return _(pods)
      .reject(p => _.isEmpty(p.deployment) || p.controlPlane)
      .groupBy("deployment")
      .map((componentPods, name) => {
        _.remove(componentPods, p => {
          return p.status == "Terminating";
        });
        let podStatuses = _.map(componentPods, p => {
          return { name: p.name, value: p.added ? "good" : "neutral" };
        });
        return { name: name, pods: _.sortBy(podStatuses, "name") }
      })
      .sortBy("name")
      .value();
  }

  processComponents(pods) {
    let ns = this.props.namespace + "/";
    let podIndex = _(pods)
      .filter(p => _.startsWith(p.deployment, ns))
      .groupBy(p => _.replace(p.deployment, ns, ""))
      .value();

    return _(componentNames)
      .map((name, id) => {
        let componentPods = _.get(podIndex, _.get(componentDeploys, id), []);
        let podStatuses = _.map(componentPods, p => {
          return { name: p.name, value: p.status === "Running" ? "good" : "bad" };
        });
        return { name: name, pods: _.sortBy(podStatuses, "name") };
      })
      .sortBy("name")
      .value();
  }

  renderControllerHealth() {
    return (
      <div className="mesh-section">
      <div className="subsection-header">Control plane status</div>
        <Row gutter={rowGutter}>
        {
          _.map(componentsToGraph, meshComponent => {
            let data = _.cloneDeep(_.find(this.state.metrics, ["name", meshComponent]) || noData);
            data.id = meshComponent;
            data.name = componentNames[meshComponent];
            return <Col span={8} key={`col-${data.id}`}>
              <DeploymentSummary
                key={data.id}
                lastUpdated={this.state.lastUpdated}
                data={data}
                noLink={true}
              />
            </Col>
          })
        }
        </Row>
      </div>
    );
  }

  renderControlPlaneDetails() {
    return (
      <div className="mesh-section">
        <div className="clearfix header-with-metric">
          <div className="subsection-header">Control plane</div>
          <Metric title="Components" value={this.componentCount()} className="metric-large" />
        </div>

        <StatusTable
          data={this.state.components}
          statusColumnTitle="Pod Status"
        />
      </div>
    );
  }

  renderDataPlaneDetails() {
    return (
      <div className="mesh-section">
        <div className="clearfix header-with-metric">
          <div className="subsection-header">Data plane</div>
          <Metric title="Proxies" value={this.proxyCount()} className="metric-large" />
          <Metric title="Deployments" value={_.size(this.state.deploys)} className="metric-large" />
        </div>

        <StatusTable
          data={this.state.deploys}
          statusColumnTitle="Proxy Status"
          shouldLink={true}
          pathPrefix={this.props.pathPrefix}
        />
      </div>
    );
  }

  renderServiceMeshDetails() {
    return (
      <div className="mesh-section">
        <div className="clearfix header-with-metric">
          <div className="subsection-header">Service mesh details</div>
        </div>

        <div className="service-mesh-table">
          <Table
            className="conduit-table"
            dataSource={this.getServiceMeshDetails()}
            columns={serviceMeshDetailsColumns}
            pagination={false}
          />
        </div>
      </div>
    );
  }

  renderAddDeploymentsMessage() {
    return (
      <div className="mesh-message">
        {this.unaddedDeploymentCount() === 0 ?
          <div className="complete-mesh-message">
            All deployments have been added to the service mesh.
          </div> 
          : this.unaddedDeploymentCount() === 1 ? 
              <div className="incomplete-mesh-message">
                1 deployment has not been added to the service mesh. 
                <div className="instructions">Add the remaining deployment to the deployment.yml file</div>
                <div className="instructions">Then run <code>conduit inject deployment.yml | kubectl apply -f - </code> to add the deployment to the service mesh</div>
              </div> 
          : <div className="incomplete-mesh-message">
              {this.unaddedDeploymentCount()} deployments have not been added to the service mesh.
              <div className="instructions">Add one or more deployments to the deployment.yml file</div>
              <div className="instructions">Then run <code>conduit inject deployment.yml | kubectl apply -f - </code> to add deploys to the service mesh</div>
            </div>
        }
      </div>
    );
  }

  renderControlPlane() {
    return (
      <Row gutter={16}>
        <Col span={16}>{this.renderControlPlaneDetails()}</Col>
        <Col span={8}>{this.renderServiceMeshDetails()}</Col>
      </Row>
    );
  }

  renderDataPlane() {
    return (
      <Row gutter={16}>
        <Col span={16}>{this.renderDataPlaneDetails()}</Col>
        <Col span={8}>{this.renderAddDeploymentsMessage()}</Col>
      </Row>
    );
  }

  renderOverview() {
    if (this.proxyCount() === 0) {
      return <CallToAction numDeployments={_.size(this.state.metrics)} />;
    } else {
      return this.renderControllerHealth();
    }
  }

  render() {
    if (!this.state.loaded) {
      return <ConduitSpinner />
    } else return (
        <div className="page-content">
          <div className="page-header">
            <h1>Service mesh overview</h1>
            {this.renderOverview()}
            {this.renderControlPlane()}
            {this.renderDataPlane()}
          </div>
        </div>
      );
    }
}
