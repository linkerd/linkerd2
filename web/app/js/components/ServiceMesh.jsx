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
        let controlPlanePods = this.processComponents(pods.pods);

        this.setState({
          deploys: podsByDeploy,
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

  addedDeploymentCount() {
    return _.size(_.filter(this.state.deploys, ["added", true]));
  }

  unaddedDeploymentCount() {
    return this.deployCount() - this.addedDeploymentCount();
  }

  proxyCount() {
    return _.sum(_.map(this.state.deploys, d => {
      return _.size(_.filter(d.pods, ["value", "good"]));
    }));
  }

  componentCount() {
    return _.size(this.state.components);
  }

  deployCount() {
    return _.size(this.state.deploys);
  }

  getServiceMeshDetails() {
    return [
      { key: 1, name: "Conduit version", value: this.props.releaseVersion },
      { key: 2, name: "Conduit namespace", value: this.props.controllerNamespace },
      { key: 3, name: "Control plane components", value: this.componentCount() },
      { key: 4, name: "Added deployments", value: this.addedDeploymentCount() },
      { key: 5, name: "Unadded deployments", value: this.unaddedDeploymentCount() },
      { key: 6, name: "Data plane proxies", value: this.proxyCount() }
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
          <div className="subsection-header">Data plane</div>
          <Metric title="Proxies" value={this.proxyCount()} className="metric-large" />
          <Metric title="Deployments" value={this.deployCount()} className="metric-large" />
        </div>

        <StatusTable
          data={this.state.deploys}
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
    if (this.deployCount() === 0) {
      return (
        <div className="mesh-completion-message">
          No deployments detected. {incompleteMeshMessage()}
        </div>
      );
    } else {
      switch (this.unaddedDeploymentCount()) {
      case 0:
        return (
          <div className="mesh-completion-message">
            All deployments have been added to the service mesh.
          </div>
        );
      case 1:
        return (
          <div className="mesh-completion-message">
            1 deployment has not been added to the service mesh. {incompleteMeshMessage()}
          </div>
        );
      default:
        return (
          <div className="mesh-completion-message">
            {this.unaddedDeploymentCount()} deployments have not been added to the service mesh. {incompleteMeshMessage()}
          </div>
        );
      }
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
      return <CallToAction numDeployments={this.deployCount()} />;
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
