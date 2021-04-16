import { Plural, Trans } from '@lingui/macro';
import { formatDistanceToNow, subSeconds } from 'date-fns';
import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import BaseTable from './BaseTable.jsx';
import CallToAction from './CallToAction.jsx';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import CheckModal from './CheckModal.jsx';
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
import { withStyles } from '@material-ui/core/styles';

const styles = {
  checkModalWrapper: {
    width: '100%',
  },
};

const serviceMeshDetailsColumns = [
  {
    title: <Trans>columnTitleName</Trans>,
    dataIndex: 'name',
  },
  {
    title: <Trans>columnTitleValue</Trans>,
    dataIndex: 'value',
    isNumeric: true,
  },
];

const getPodClassification = pod => {
  if (pod.status === 'Running') {
    return 'good';
  } else if (pod.status === 'Waiting') {
    return 'default';
  } else {
    return 'poor';
  }
};

const componentsToDeployNames = {
  Destination: 'linkerd-destination',
  Identity: 'linkerd-identity',
  'Proxy Injector': 'linkerd-proxy-injector',
};

class ServiceMesh extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);
    this.api = props.api;

    this.state = {
      pollingInterval: 2000,
      components: [],
      nsStatuses: [],
      pendingRequests: false,
      loaded: false,
      error: null,
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentDidUpdate(prevProps) {
    const { isPageVisible } = this.props;
    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  getServiceMeshDetails() {
    const { components } = this.state;
    const { productName, releaseVersion, controllerNamespace } = this.props;

    return [
      { key: 1, name: <Trans>{productName} version</Trans>, value: releaseVersion },
      { key: 2, name: <Trans>{productName} namespace</Trans>, value: controllerNamespace },
      { key: 3, name: <Trans>Control plane components</Trans>, value: components.length },
      { key: 4, name: <Trans>Data plane proxies</Trans>, value: this.proxyCount() },
    ];
  }

  getControllerComponentData = podData => {
    const podDataByDeploy = _groupBy(_filter(podData.pods, d => d.controlPlane), p => p.deployment);
    const byDeployName = _mapKeys(podDataByDeploy, (_pods, dep) => dep.split('/')[1]);

    return _map(componentsToDeployNames, (deployName, component) => {
      return {
        name: component,
        pods: _map(byDeployName[deployName], p => {
          const uptimeSec = !p.uptime ? 0 : parseInt(p.uptime.split('.')[0], 10);
          const uptime = formatDistanceToNow(subSeconds(Date.now(), uptimeSec));

          return {
            name: p.name,
            value: getPodClassification(p),
            uptime,
            uptimeSec,
          };
        }),
      };
    });
  }

  startServerPolling() {
    const { pollingInterval } = this.state;
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({ pendingRequests: false });
  }

  extractNsStatuses = nsData => {
    const podsByNs = _get(nsData, ['ok', 'statTables', 0, 'podGroup', 'rows'], []);
    const dataPlaneNamespaces = podsByNs.map(ns => {
      const meshedPods = parseInt(ns.meshedPodCount, 10);
      const totalPods = parseInt(ns.runningPodCount, 10);
      const failedPods = parseInt(ns.failedPodCount, 10);

      return {
        namespace: ns.resource.name,
        meshedPodsStr: `${ns.meshedPodCount}/${ns.runningPodCount}`,
        meshedPercent: new Percentage(meshedPods, totalPods),
        meshedPods,
        totalPods,
        failedPods,
        errors: ns.errorsByPod,
      };
    });
    return _compact(dataPlaneNamespaces);
  }

  loadFromServer() {
    const { pendingRequests } = this.state;
    const { controllerNamespace } = this.props;

    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchPods(controllerNamespace),
      this.api.fetchMetrics(this.api.urlsForResourceNoStats('namespace')),
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([pods, nsStats]) => {
        this.setState({
          components: this.getControllerComponentData(pods),
          nsStatuses: this.extractNsStatuses(nsStats),
          pendingRequests: false,
          loaded: true,
          error: null,
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
      error: e,
    });
  }

  proxyCount() {
    const { nsStatuses } = this.state;
    const { controllerNamespace } = this.props;

    return _sumBy(nsStatuses, d => {
      return d.namespace === controllerNamespace ? 0 : d.meshedPods;
    });
  }

  renderControlPlaneDetails() {
    const { components } = this.state;

    return (
      <React.Fragment>
        <Grid container justify="space-between">
          <Grid item xs={3}>
            <Typography variant="h6"><Trans>Control plane</Trans></Typography>
          </Grid>
          <Grid item xs={3}>
            <Typography align="right"><Trans>componentsMsg</Trans></Typography>
            <Typography align="right">{components.length}</Typography>
          </Grid>
        </Grid>

        <StatusTable
          data={components}
          statusColumnTitle="Pod Status"
          shouldLink={false} />
      </React.Fragment>
    );
  }

  renderServiceMeshDetails() {
    return (
      <React.Fragment>
        <Typography variant="h6"><Trans>Service mesh details</Trans></Typography>

        <BaseTable
          tableClassName="metric-table"
          tableRows={this.getServiceMeshDetails()}
          tableColumns={serviceMeshDetailsColumns}
          rowKey={d => d.key} />

      </React.Fragment>
    );
  }

  renderAddResourcesMessage() {
    const { nsStatuses } = this.state;
    const { productName } = this.props;

    let message = '';
    let numUnadded = 0;

    if (_isEmpty(nsStatuses)) {
      message = <Trans>noNamespacesDetectedMsg</Trans>;
    } else {
      const meshedCount = _countBy(nsStatuses, pod => {
        return pod.meshedPercent.get() > 0;
      });
      numUnadded = meshedCount.false || 0;
      message = numUnadded === 0 ? <Trans>All namespaces have a {productName} install.</Trans> : (
        <Plural
          value={numUnadded}
          one="# namespace has no meshed resources"
          other="# namespaces have no meshed resources" />
      );
    }

    return (
      <Card elevation={3}>
        <CardContent>
          <Typography variant="body2">{message}</Typography>
          { numUnadded > 0 ? incompleteMeshMessage() : null }
        </CardContent>
      </Card>
    );
  }

  render() {
    const { error, loaded, nsStatuses } = this.state;
    const { classes } = this.props;

    return (
      <div className="page-content">
        { !error ? null : <ErrorBanner message={error} /> }
        { !loaded ? <Spinner /> : (
          <div>
            {this.proxyCount() === 0 ?
              <CallToAction
                numResources={nsStatuses.length}
                resource="namespace" /> : null}

            <Grid container spacing={3}>
              <Grid item xs={8} container direction="column">
                <Grid item>{this.renderControlPlaneDetails()}</Grid>
                <Grid item>
                  <MeshedStatusTable tableRows={nsStatuses} />
                </Grid>
              </Grid>

              <Grid item xs={4} container direction="column" spacing={3}>
                <Grid item>{this.renderServiceMeshDetails()}</Grid>
                <Grid className={classes.checkModalWrapper} item><CheckModal api={this.api} /></Grid>
                <Grid item>{this.renderAddResourcesMessage()}</Grid>
              </Grid>
            </Grid>
          </div>
        )}
      </div>
    );
  }
}

ServiceMesh.propTypes = {
  api: PropTypes.shape({
    cancelCurrentRequests: PropTypes.func.isRequired,
    PrefixedLink: PropTypes.func.isRequired,
    fetchMetrics: PropTypes.func.isRequired,
    getCurrentPromises: PropTypes.func.isRequired,
    setCurrentRequests: PropTypes.func.isRequired,
    urlsForResourceNoStats: PropTypes.func.isRequired,
  }).isRequired,
  controllerNamespace: PropTypes.string.isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  productName: PropTypes.string,
  releaseVersion: PropTypes.string.isRequired,
};

ServiceMesh.defaultProps = {
  productName: 'controller',
};

export default withPageVisibility(withStyles(styles)(withContext(ServiceMesh)));
