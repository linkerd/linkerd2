import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import Metric from './Metric.jsx';
import PageHeader from './PageHeader.jsx';
import React from 'react';
import StatusTable from './StatusTable.jsx';
import { Col, Row, Table } from 'antd';
import {
  getComponentPods,
  getPodsByDeployment,
  getPodsByResource,
} from './util/MetricUtils.js';
import './../../css/service-mesh.css';

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
  "destination":  "Destination",
  "proxy-api":    "Proxy API",
  "public-api":   "Public API",
  "tap":          "Tap",
  "web":          "Web UI"
};

const componentDeploys = {
  "prometheus":   "prometheus",
  "destination":  "controller",
  "proxy-api":    "controller",
  "public-api":   "controller",
  "tap":          "controller",
  "web":          "web"
};

export default class ServiceMesh extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);
    this.api = this.props.api;

    this.state = {
      pollingInterval: 2000,
      metrics: [],
      deploys: [],
      components: [],
      lastUpdated: 0,
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

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchPods()
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([pods]) => {
        let podsByDeploy = getPodsByDeployment(pods.pods);
        let podsByReplicationController = getPodsByResource(pods.pods, "replicationController");
        let controlPlanePods = this.processComponents(pods.pods);

        this.setState({
          deploys: podsByDeploy,
          replicationControllers: podsByReplicationController,
          components: controlPlanePods,
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

  addedCount(resource) {
    return _.size(_.filter(this.state[resource], ["added", true]));
  }

  unaddedCount(resource) {
    return this.resourceCount(resource) - this.addedCount(resource);
  }

  proxyCount() {
    return this.resourceProxyCount("deploys") + this.resourceProxyCount("replicationControllers");
  }

  resourceProxyCount(resource) {
    return _.sum(_.map(this.state[resource], d => {
      return _.size(_.filter(d.pods, ["value", "good"]));
    }));
  }

  componentCount() {
    return _.size(this.state.components);
  }

  resourceCount(resource) {
    return _.size(this.state[resource]);
  }

  getServiceMeshDetails() {
    return [
      { key: 1, name: "Conduit version", value: this.props.releaseVersion },
      { key: 2, name: "Conduit namespace", value: this.props.controllerNamespace },
      { key: 3, name: "Control plane components", value: this.componentCount() },
      { key: 4, name: "Added deployments", value: this.addedCount("deploys") },
      { key: 5, name: "Unadded deployments", value: this.unaddedCount("deploys") },
      { key: 6, name: "Added replication controllers", value: this.addedCount("replicationControllers") },
      { key: 7, name: "Unadded replication controllers", value: this.unaddedCount("replicationControllers") },
      { key: 8, name: "Data plane proxies", value: this.proxyCount() }
    ];
  }

  processComponents(pods) {
    let podIndex = _(pods)
      .filter(p => p.controlPlane)
      .groupBy(p => _.last(_.split(p.deployment, "/")))
      .value();

    return _(componentNames)
      .map((name, id) => {
        let componentPods = _.get(podIndex, _.get(componentDeploys, id), []);
        return { name: name, pods: getComponentPods(componentPods) };
      })
      .sortBy("name")
      .value();
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
          shouldLink={false}
          api={this.api} />
      </div>
    );
  }

  renderDataPlaneDetails() {
    return (
      <div className="mesh-section">
        <div className="clearfix header-with-metric">
          <div className="subsection-header">Data plane: deployments</div>
          <Metric title="Proxies" value={this.resourceProxyCount("deploys")} className="metric-large" />
          <Metric title="Deployments" value={this.resourceCount("deploys")} className="metric-large" />
        </div>

        <StatusTable
          data={this.state.deploys}
          resource="Deployment"
          statusColumnTitle="Proxy Status"
          shouldLink={true}
          api={this.api}  />

        <div className="clearfix header-with-metric">
          <div className="subsection-header">Data plane: Replication Controllers</div>
          <Metric title="Proxies" value={this.resourceProxyCount("replicationControllers")} className="metric-large" />
          <Metric title="RCs" value={this.resourceCount("replicationControllers")} className="metric-large" />
        </div>

        <StatusTable
          data={this.state.replicationControllers}
          resource="Replication Controller"
          statusColumnTitle="Proxy Status"
          shouldLink={true}
          api={this.api}  />
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
            size="middle" />
        </div>
      </div>
    );
  }

  renderAddDeploymentsMessage() {
    if (this.resourceCount("deploys") === 0 && this.resourceCount("replicationControllers") === 0) {
      return (
        <div className="mesh-completion-message">
          No deployments or replication controllers detected. {incompleteMeshMessage()}
        </div>
      );
    } else {
      let needsIncompleteMessage = false;
      let deployMessage;
      let rcMessage;

      switch (this.unaddedCount("deploys")) {
      case 0:
        deployMessage = (
          <div>
            All deployments have been added to the service mesh.
          </div>
        );
        break;
      case 1:
        needsIncompleteMessage = true;
        deployMessage =  (
          <div>
            1 deployment has not been added to the service mesh.
          </div>
        );
        break;
      default:
        needsIncompleteMessage = true;
        deployMessage = (
          <p>{this.unaddedCount("deploys")} deployments have not been added to the service mesh.</p>
        );
      }

      switch (this.unaddedCount("replicationControllers")) {
      case 0:
        rcMessage = (
          <div>
              All resource controllers have been added to the service mesh.
          </div>
        );
        break;
      case 1:
        needsIncompleteMessage = true;
        rcMessage =  (
          <div>
              1 resource controller has not been added to the service mesh.
          </div>
        );
        break;
      default:
        needsIncompleteMessage = true;
        rcMessage = (
          <p>{this.unaddedCount("replicationControllers")} replication controllers have not been added to the service mesh.</p>
        );
      }

      return (
        <div className="mesh-completion-message">
          {deployMessage}<br />
          {rcMessage}<br />
          {needsIncompleteMessage ? incompleteMeshMessage("", "resource") : null}
        </div>
      );
    }
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
      return (<CallToAction
        resource="resource"
        numDeployments={this.resourceCount("deploys")}
        numReplicationControllers={this.resourceCount("replicationControllers")} />);
    }
  }

  render() {
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner /> :
          <div>
            <PageHeader
              header="Service mesh overview"
              hideButtons={this.proxyCount() === 0}
              api={this.api} />
            {this.renderOverview()}
            {this.renderControlPlane()}
            {this.renderDataPlane()}
          </div>
        }
      </div>
    );
  }
}
