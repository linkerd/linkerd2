import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import ErrorModal from './ErrorModal.jsx';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import Metric from './Metric.jsx';
import moment from 'moment';
import { numericSort } from './util/Utils.js';
import Percentage from './util/Percentage.js';
import PropTypes from 'prop-types';
import React from 'react';
import StatusTable from './StatusTable.jsx';
import { withContext } from './util/AppContext.jsx';
import { Col, Row, Table, Tooltip } from 'antd';
import Spinner from './util/Spinner.jsx';
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

const getClassification = (meshedPodCount, failedPodCount) => {
  if (failedPodCount > 0) {
    return "poor";
  } else if (meshedPodCount === 0) {
    return "neutral";
  } else {
    return "good";
  }
};

const getPodClassification = pod => {
  if (pod.status === "Running") {
    return "good";
  } else if (pod.status === "Waiting") {
    return "neutral";
  } else {
    return "poor";
  }
};

const namespacesColumns = PrefixedLink => [
  {
    title: "Namespace",
    key: "namespace",
    defaultSortOrder: "ascend",
    sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace),
    render: d => {
      return  (
        <React.Fragment>
          <PrefixedLink to={"/namespaces/" + d.namespace}>{d.namespace}</PrefixedLink>
          { _.isEmpty(d.errors) ? null :
          <ErrorModal errors={d.errors} resourceName={d.namespace} resourceType="namespace" />
          }
        </React.Fragment>
      );
    }
  },
  {
    title: "Meshed pods",
    dataIndex: "meshedPodsStr",
    key: "meshedPodsStr",
    className: "numeric",
    sorter: (a, b) => numericSort(a.totalPods, b.totalPods),
  },
  {
    title: "Meshed Status",
    key: "meshification",
    sorter: (a, b) => numericSort(a.meshedPercent.get(), b.meshedPercent.get()),
    render: row => {
      let containerWidth = 132;
      let percent = row.meshedPercent.get();
      let barWidth = percent < 0 ? 0 : Math.round(percent * containerWidth);
      let barType = _.isEmpty(row.errors) ?
        getClassification(row.meshedPods, row.failedPods) : "poor";


      let percentMeshedMsg = "";
      if (row.meshedPercent.get() >= 0) {
        percentMeshedMsg = `(${row.meshedPercent.prettyRate()})`;
      }
      return (
        <Tooltip
          overlayStyle={{ fontSize: "12px" }}
          title={(
            <div>
              <div>
                {`${row.meshedPods} out of ${row.totalPods} running or pending pods are in the mesh ${percentMeshedMsg}`}
              </div>
              {row.failedPods === 0 ? null : <div>{ `${row.failedPods} failed pods` }</div>}
            </div>
            )}>
          <div className={"container-bar " + barType} style={{width: containerWidth}}>
            <div className={"inner-bar " + barType} style={{width: barWidth}}>&nbsp;</div>
          </div>
        </Tooltip>
      );
    }
  }
];

const componentsToDeployNames = {
  "Destination": "controller",
  "Grafana" : "grafana",
  "Prometheus": "prometheus",
  "Proxy API": "controller",
  "Public API": "controller",
  "Tap": "controller",
  "Web UI": "web"
};

class ServiceMesh extends React.Component {
  static defaultProps = {
    productName: 'controller'
  }

  static propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      PrefixedLink: PropTypes.func.isRequired,
      fetchMetrics: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
      urlsForResource: PropTypes.func.isRequired,
    }).isRequired,
    controllerNamespace: PropTypes.string.isRequired,
    productName: PropTypes.string,
    releaseVersion: PropTypes.string.isRequired,
  }

  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);
    this.api = this.props.api;

    this.state = {
      pollingInterval: 2000,
      components: [],
      pendingRequests: false,
      loaded: false,
      error: null
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

  getServiceMeshDetails() {
    return [
      { key: 1, name: this.props.productName + " version", value: this.props.releaseVersion },
      { key: 2, name: this.props.productName + " namespace", value: this.props.controllerNamespace },
      { key: 3, name: "Control plane components", value: this.componentCount() },
      { key: 4, name: "Data plane proxies", value: this.proxyCount() }
    ];
  }

  getControllerComponentData(podData) {
    let podDataByDeploy = _.chain(podData.pods)
      .filter( "controlPlane")
      .groupBy("deployment")
      .mapKeys((pods, dep) => {
        return dep.split("/")[1];
      })
      .value();

    return _.map(componentsToDeployNames, (deployName, component) => {
      return {
        name: component,
        pods: _.map(podDataByDeploy[deployName], p => {
          let uptimeSec = !p.uptime ? 0 : p.uptime.split(".")[0];
          let uptime = moment.duration(parseInt(uptimeSec, 10) * 1000);

          return {
            name: p.name,
            value: getPodClassification(p),
            uptime: uptime.humanize(),
            uptimeSec
          };
        })
      };
    });
  }

  extractNsStatuses(nsData) {
    let podsByNs = _.get(nsData, ["ok", "statTables", 0, "podGroup", "rows"], []);
    let dataPlaneNamepaces = _.map(podsByNs, ns => {
      let meshedPods = parseInt(ns.meshedPodCount, 10);
      let totalPods = parseInt(ns.runningPodCount, 10);
      let failedPods = parseInt(ns.failedPodCount, 10);

      return {
        namespace: ns.resource.name,
        meshedPodsStr: ns.meshedPodCount + "/" + ns.runningPodCount,
        meshedPercent: new Percentage(meshedPods, totalPods),
        meshedPods,
        totalPods,
        failedPods,
        errors: ns.errorsByPod
      };
    });
    return _.compact(dataPlaneNamepaces);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchPods(this.props.controllerNamespace),
      this.api.fetchMetrics(this.api.urlsForResource("namespace"))
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([pods, nsStats]) => {
        this.setState({
          components: this.getControllerComponentData(pods),
          nsStatuses: this.extractNsStatuses(nsStats),
          pendingRequests: false,
          loaded: true,
          error: null
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
      loaded: true,
      error: e
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
            className="metric-table"
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
          All namespaces have a {this.props.productName} install.
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
              className="metric-table service-mesh-table mesh-completion-table"
              dataSource={this.state.nsStatuses}
              columns={namespacesColumns(this.api.PrefixedLink)}
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
        { !this.state.loaded ? <Spinner /> : (
          <div>
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
        )}
      </div>
    );
  }
}

export default withContext(ServiceMesh);
