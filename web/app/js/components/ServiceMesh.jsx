import { distanceInWordsToNow, subSeconds } from 'date-fns';
import BaseTable from './BaseTable.jsx';
import CallToAction from './CallToAction.jsx';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import MeshedStatusTable from './MeshedStatusTable.jsx';
import Percentage from './util/Percentage.js';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import StatusTable from './StatusTable.jsx';
import Typography from '@material-ui/core/Typography';
import _compact from 'lodash/compact';
import _countBy from 'lodash/countBy';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _groupBy from 'lodash/groupBy';
import _isEmpty from 'lodash/isEmpty';
import _map from 'lodash/map';
import _mapKeys from 'lodash/mapKeys';
import _sumBy from 'lodash/sumBy';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const serviceMeshDetailsColumns = [
  {
    title: "Name",
    dataIndex: "name"
  },
  {
    title: "Value",
    dataIndex: "value",
    isNumeric: true
  }
];

const getPodClassification = pod => {
  if (pod.status === "Running") {
    return "good";
  } else if (pod.status === "Waiting") {
    return "default";
  } else {
    return "poor";
  }
};

const componentsToDeployNames = {
  "Destination": "linkerd-controller",
  "Grafana": "linkerd-grafana",
  "Identity": "linkerd-identity",
  "Prometheus": "linkerd-prometheus",
  "Public API": "linkerd-controller",
  "Service Profile Validator": "linkerd-sp-validator",
  "Tap": "linkerd-controller",
  "Web UI": "linkerd-web"
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
      nsStatuses: [],
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
    let podDataByDeploy = _groupBy(_filter(podData.pods, d => d.controlPlane), p => p.deployment);
    let byDeployName = _mapKeys(podDataByDeploy, (_pods, dep) => dep.split("/")[1]);

    return _map(componentsToDeployNames, (deployName, component) => {
      return {
        name: component,
        pods: _map(byDeployName[deployName], p => {
          let uptimeSec = !p.uptime ? 0 : p.uptime.split(".")[0];
          let uptime = distanceInWordsToNow(subSeconds(Date.now(), parseInt(uptimeSec, 10)));

          return {
            name: p.name,
            value: getPodClassification(p),
            uptime,
            uptimeSec
          };
        })
      };
    });
  }

  extractNsStatuses(nsData) {
    let podsByNs = _get(nsData, ["ok", "statTables", 0, "podGroup", "rows"], []);
    let dataPlaneNamepaces = podsByNs.map(ns => {
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
    return _compact(dataPlaneNamepaces);
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
    return this.state.components.length;
  }

  proxyCount() {
    return _sumBy(this.state.nsStatuses, d => {
      return d.namespace === this.props.controllerNamespace ? 0 : d.meshedPods;
    });
  }

  renderControlPlaneDetails() {
    return (
      <React.Fragment>
        <Grid container justify="space-between">
          <Grid item xs={3}>
            <Typography variant="h6">Control plane</Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography align="right">Components</Typography>
            <Typography align="right">{this.componentCount()}</Typography>
          </Grid>
        </Grid>

        <StatusTable
          data={this.state.components}
          statusColumnTitle="Pod Status"
          shouldLink={false}
          api={this.api} />
      </React.Fragment>
    );
  }

  renderServiceMeshDetails() {
    return (
      <React.Fragment>
        <Typography variant="h6">Service mesh details</Typography>

        <BaseTable
          tableClassName="metric-table"
          tableRows={this.getServiceMeshDetails()}
          tableColumns={serviceMeshDetailsColumns}
          rowKey={d => d.key} />

      </React.Fragment>
    );
  }

  renderAddResourcesMessage() {
    let message = "";
    let numUnadded = 0;

    if (_isEmpty(this.state.nsStatuses)) {
      message = "No resources detected.";
    } else {
      let meshedCount = _countBy(this.state.nsStatuses, pod => {
        return pod.meshedPercent.get() > 0;
      });
      numUnadded = meshedCount["false"] || 0;
      message = numUnadded === 0 ? `All namespaces have a ${this.props.productName} install.` :
        `${numUnadded} ${numUnadded === 1 ? "namespace has" : "namespaces have"} no meshed resources.`;
    }

    return (
      <Card>
        <CardContent>
          <Typography>{message}</Typography>
          { numUnadded > 0 ? incompleteMeshMessage() : null }
        </CardContent>
      </Card>
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
                numResources={this.state.nsStatuses.length}
                resource="namespace" /> : null}

            <Grid container spacing={24}>
              <Grid item xs={8} container direction="column" >
                <Grid item>{this.renderControlPlaneDetails()}</Grid>
                <Grid item>
                  <MeshedStatusTable tableRows={this.state.nsStatuses} />
                </Grid>
              </Grid>

              <Grid item xs={4} container direction="column" spacing={24}>
                <Grid item>{this.renderServiceMeshDetails()}</Grid>
                <Grid item>{this.renderAddResourcesMessage()}</Grid>
              </Grid>
            </Grid>
          </div>
        )}
      </div>
    );
  }
}

export default withContext(ServiceMesh);
