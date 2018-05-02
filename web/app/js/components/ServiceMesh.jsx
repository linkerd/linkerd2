import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import Metric from './Metric.jsx';
import { numericSort } from './util/Utils.js';
import PageHeader from './PageHeader.jsx';
import Percentage from './util/Percentage.js';
import React from 'react';
import StatusTable from './StatusTable.jsx';
import { Col, Row, Table, Tooltip } from 'antd';
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

const barColor = percentMeshed => {
  if (percentMeshed <= 0) {
    return "neutral";
  } else {
    return "good";
  }
};

const namespacesColumns = ConduitLink => [
  {
    title: "Namespace",
    dataIndex: "namespace",
    key: "namespace",
    defaultSortOrder: "ascend",
    sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace),
    render: d => <ConduitLink to={"/namespaces?ns=" + d}>{d}</ConduitLink>
  },
  {
    title: "Meshed pods",
    dataIndex: "meshedPodsStr",
    key: "meshedPodsStr",
    className: "numeric",
    sorter: (a, b) => numericSort(a.totalPods, b.totalPods),
  },
  {
    title: "Mesh completion",
    key: "meshification",
    sorter: (a, b) => numericSort(a.meshedPercent.get(), b.meshedPercent.get()),
    render: row => {
      let containerWidth = 132;
      let percent = row.meshedPercent.get();
      let barWidth = percent < 0 ? 0 : Math.round(percent * containerWidth);
      let barType = barColor(percent);

      return (
        <Tooltip
          overlayStyle={{ fontSize: "12px" }}
          title={<div>
            <div>
              {`${row.meshedPods} / ${row.totalPods} pods in mesh (${row.meshedPercent.prettyRate()})`}
            </div>
          </div>}>
          <div className={"container-bar " + barType} style={{width: containerWidth}}>
            <div className={"inner-bar " + barType} style={{width: barWidth}}>&nbsp;</div>
          </div>
        </Tooltip>
      );
    }
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

  extractNsStatuses(nsData) {
    let podsByNs = _.get(nsData, ["ok", "statTables", 0, "podGroup", "rows"], []);
    let dataPlaneNamepaces = _.map(podsByNs, ns => {
      if (ns.resource.name.indexOf("kube-") === 0) {
        return;
      }
      let meshedPods = parseInt(ns.meshedPodCount, 10);
      let totalPods = parseInt(ns.totalPodCount, 10);

      return {
        namespace: ns.resource.name,
        meshedPodsStr: ns.meshedPodCount + "/" + ns.totalPodCount,
        meshedPercent: new Percentage(meshedPods, totalPods),
        meshedPods,
        totalPods
      };
    });
    return _.compact(dataPlaneNamepaces);
  }

  processComponents(conduitPods) {
    let pods = _.get(conduitPods, ["ok", "statTables", 0, "podGroup", "rows"], 0);
    return _.map(componentNames, (title, name) => {
      let deployName = componentDeploys[name];
      let matchingPods = _.filter(pods, p => p.resource.name.split("-")[0] === deployName);

      return {
        name: title,
        pods: _.map(matchingPods, p => {
          return {
            name: p.resource.name,
            // we need an endpoint to return the k8s status of these pods
            value: _.size(matchingPods) > 0 ? "good" : "neutral"
          };
        })
      };
    });
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource["pod"].url(this.props.controllerNamespace).rollup),
      this.api.fetchMetrics(this.api.urlsForResource["namespace"].url().rollup)
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([conduitPods, nsStats]) => {
        this.setState({
          components: this.processComponents(conduitPods),
          nsStatuses: this.extractNsStatuses(nsStats),
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

  componentCount() {
    return _.size(this.state.components);
  }

  proxyCount() {
    return _.sumBy(this.state.nsStatuses, d => {
      return d.namespace === this.props.controllerNamespace ? 0 : d.meshedPods;
    });
  }

  getServiceMeshDetails() {
    return [
      { key: 1, name: "Conduit version", value: this.props.releaseVersion },
      { key: 2, name: "Conduit namespace", value: this.props.controllerNamespace },
      { key: 3, name: "Control plane components", value: this.componentCount() },
      { key: 4, name: "Data plane proxies", value: this.proxyCount() }
    ];
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

  renderAddResourcesMessage() {
    if (_.isEmpty(this.state.nsStatuses)) {
      return <div className="mesh-completion-message">No resources detected.</div>;
    }

    let meshedCount = _.countBy(this.state.nsStatuses, pod => {
      return pod.meshedPercent.get() > 0;
    });
    let numUnadded = meshedCount["false"] || 0;

    if (numUnadded === 0) {
      return (
        <div className="mesh-completion-message">
          All namespaces have a conduit install.
        </div>
      );
    } else {
      return (
        <div className="mesh-completion-message">
          {numUnadded} {numUnadded === 1 ? "namespace has" : "namespaces have"} no meshed resources.
          {incompleteMeshMessage()}
        </div>
      );
    }
  }

  renderNamespaceStatusTable() {
    let rowCn = row => {
      return row.meshedPercent.get() > 0.9 ? "good" : "neutral";
    };

    return (
      <div className="mesh-section">
        <Row gutter={16}>
          <Col span={16}>
            <Table
              className="conduit-table service-mesh-table mesh-completion-table"
              dataSource={this.state.nsStatuses}
              columns={namespacesColumns(this.api.ConduitLink)}
              rowKey="namespace"
              rowClassName={rowCn}
              pagination={false}
              size="middle" />
          </Col>
          <Col span={8}>{this.renderAddResourcesMessage()}</Col>
        </Row>
      </div>
    );
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

            {this.proxyCount() === 0 ?
              <CallToAction
                numResources={_.size(this.state.nsStatuses)}
                resource="namespace" /> : null}

            <Row gutter={16}>
              <Col span={16}>{this.renderControlPlaneDetails()}</Col>
              <Col span={8}>{this.renderServiceMeshDetails()}</Col>
            </Row>

            {this.renderNamespaceStatusTable()}
          </div>
        }
      </div>
    );
  }
}
