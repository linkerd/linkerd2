import 'whatwg-fetch';

import { processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';

import Accordion from './util/Accordion.jsx';
import Divider from '@material-ui/core/Divider';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import Hidden from '@material-ui/core/Hidden';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SimpleChip from './util/Chip.jsx';
import Spinner from './util/Spinner.jsx';
import Typography from '@material-ui/core/Typography';
import _find from 'lodash/find';
import _get from 'lodash/get';
import _has from 'lodash/has';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import { friendlyTitle } from './util/Utils.js';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = () => {
  return {
    grid: {
      width: '100%'
    }
  };
};

class NamespaceLanding extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      fetchMetrics: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
      urlsForResource: PropTypes.func.isRequired,
      urlsForResourceNoStats: PropTypes.func.isRequired,
    }).isRequired,
    classes: PropTypes.shape({}).isRequired,
    controllerNamespace: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      selectedNs: null,
      metricsByNs: {},
      namespaces: [],
      pollingInterval: 2000,
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

  onNamespaceChange = ns => {
    this.setState({
      selectedNs: ns
    });
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    // TODO: make this one request
    let apiRequests = [
      this.api.fetchMetrics(this.api.urlsForResourceNoStats("namespace"))
    ];
    if (!_isEmpty(this.state.selectedNs)) {
      apiRequests = apiRequests.concat([
        this.api.fetchMetrics(this.api.urlsForResource("all", this.state.selectedNs, true))
      ]);
    }
    this.api.setCurrentRequests(apiRequests);

    Promise.all(this.api.getCurrentPromises())
      .then(([allNs, metricsForNs]) => {
        let namespaces = processSingleResourceRollup(allNs);
        let metricsByNs = this.state.metricsByNs;
        if (!_isNil(this.state.selectedNs) && !_isNil(metricsForNs)) {
          metricsByNs[this.state.selectedNs] = processMultiResourceRollup(metricsForNs);
        }

        // by default, show the first non-linkerd meshed namesapce
        // if no other meshed namespaces are found, show the linkerd namespace
        let defaultOpenNs = _find(namespaces, ns => ns.added && ns.name !== this.props.controllerNamespace);
        defaultOpenNs = defaultOpenNs || _find(namespaces, ns => ns.name === this.props.controllerNamespace);

        this.setState({
          namespaces,
          metricsByNs,
          defaultOpenNs,
          selectedNs: this.state.selectedNs || defaultOpenNs.name,
          pendingRequests: false,
          loaded: true,
          error: null
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e
    });
  }

  renderResourceSection(resource, metrics) {
    const classes = this.props.classes;
    if (_isEmpty(metrics)) {
      return null;
    }

    return (
      <Grid container direction="column" justify="center">
        <Grid className={classes.grid} item>
          <MetricsTable
            title={friendlyTitle(resource).plural}
            resource={resource}
            metrics={metrics}
            showNamespaceColumn={false} />
        </Grid>
      </Grid>
    );
  }

  renderNamespaceSection(namespace) {
    const classes = this.props.classes;
    if (!_has(this.state.metricsByNs, namespace)) {
      return <Spinner />;
    }

    let metrics = this.state.metricsByNs[namespace] || {};
    let noMetrics = _isEmpty(metrics.pod);

    return (
      <Grid container direction="column" spacing={16}>
        <Hidden smDown>
          <Grid item><Typography variant="h4">Namespace: {namespace}</Typography></Grid>
          <Grid item><Divider /></Grid>
        </Hidden>
        <Grid item>{noMetrics ? <div>No resources detected.</div> : null}</Grid>

        {this.renderResourceSection("deployment", metrics.deployment)}
        {this.renderResourceSection("daemonset", metrics.daemonset)}
        {this.renderResourceSection("pod", metrics.pod)}
        {this.renderResourceSection("replicationcontroller", metrics.replicationcontroller)}
        {this.renderResourceSection("statefulset", metrics.statefulset)}
        {this.renderResourceSection("job", metrics.job)}

        {
          noMetrics ? null :
          <Grid container direction="column" justify="center">
            <Grid className={classes.grid} item>
              <MetricsTable
                title="TCP"
                resource="pod"
                metrics={metrics.pod}
                isTcpTable={true} />
            </Grid>
          </Grid>
        }
      </Grid>
    );
  }

  renderAccordion() {
    let panelData = this.state.namespaces.map(ns => {
      let hr = (
        <Grid container justify="space-between" alignItems="center">
          <Grid item><Typography variant="subtitle1">{ns.name}</Typography></Grid>
          {!ns.added ? null : <Grid item><SimpleChip label="meshed" type="good" /></Grid>}
        </Grid>
      );
      return {
        id: ns.name,
        header: hr,
        body: ns.name === this.state.selectedNs || ns.name === this.state.defaultOpenNs.name ?
          this.renderNamespaceSection(ns.name) : null
      };
    });

    return (
      <Grid container>

        <Accordion
          onChange={this.onNamespaceChange}
          panels={panelData}
          defaultOpenPanel={_get(this.state.defaultOpenNs, 'name', null)} />
      </Grid>
    );
  }

  render() {
    return (
      <div className="page-content">
        {!this.state.error ? null : <ErrorBanner message={this.state.error} />}
        {!this.state.loaded ? <Spinner /> : this.renderAccordion()}
      </div>);
  }
}

export default withContext(withStyles(styles)(NamespaceLanding));
