import 'whatwg-fetch';

import { processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';

import Accordion from './util/Accordion.jsx';
import CheckCircleIcon from '@material-ui/icons/CheckCircle';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import Tooltip from '@material-ui/core/Tooltip';
import Typography from '@material-ui/core/Typography';
import _ from 'lodash';
import { friendlyTitle } from './util/Utils.js';
import { withContext } from './util/AppContext.jsx';

const isMeshedTooltip = (
  <Tooltip title="Namespace is meshed" placement="right-start">
    <CheckCircleIcon color="primary" />
  </Tooltip>
);
class NamespaceLanding extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      fetchMetrics: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
      urlsForResource: PropTypes.func.isRequired,
    }).isRequired,
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
      this.api.fetchMetrics(this.api.urlsForResource("namespace"))
    ];
    if (!_.isEmpty(this.state.selectedNs)) {
      apiRequests = _.concat(apiRequests, [this.api.fetchMetrics(this.api.urlsForResource("all", this.state.selectedNs))]);
    }
    this.api.setCurrentRequests(apiRequests);

    Promise.all(this.api.getCurrentPromises())
      .then(([allNs, metricsForNs]) => {
        let namespaces = processSingleResourceRollup(allNs);
        let metricsByNs = this.state.metricsByNs;
        if (!_.isNil(this.state.selectedNs) && !_.isNil(metricsForNs)) {
          metricsByNs[this.state.selectedNs] = processMultiResourceRollup(metricsForNs);
        }

        // by default, show the first non-linkerd meshed namesapce
        // if no other meshed namespaces are found, show the linkerd namespace
        let defaultOpenNs = _.find(namespaces, ns => ns.added && ns.name !== this.props.controllerNamespace);
        defaultOpenNs = defaultOpenNs || _.find(namespaces, ['name', this.props.controllerNamespace]);

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
    if (_.isEmpty(metrics)) {
      return null;
    }
    return (
      <Grid container direction="column" justify="center">
        <Grid item>
          <Typography variant="h5">{friendlyTitle(resource).plural}</Typography>
        </Grid>

        <Grid item>
          <MetricsTable
            resource={resource}
            metrics={metrics}
            showNamespaceColumn={false} />
        </Grid>
      </Grid>
    );
  }

  renderNamespaceSection(namespace) {
    if (!_.has(this.state.metricsByNs, namespace)) {
      return <Spinner />;
    }

    let metrics = this.state.metricsByNs[namespace] || {};
    let noMetrics = _.isEmpty(metrics.pod);

    return (
      <Grid container direction="column">
        <Grid item><Typography variant="h4">Namespace: {namespace}</Typography></Grid>
        <Grid item>{ noMetrics ? <div>No resources detected.</div> : null}</Grid>

        {this.renderResourceSection("deployment", metrics.deployment)}
        {this.renderResourceSection("replicationcontroller", metrics.replicationcontroller)}
        {this.renderResourceSection("pod", metrics.pod)}
        {this.renderResourceSection("authority", metrics.authority)}
      </Grid>
    );
  }

  renderAccordion() {
    let panelData = _.map(this.state.namespaces, ns => {
      return {
        id: ns.name,
        header: <React.Fragment><Typography variant="subtitle1">{ns.name}</Typography> {!ns.added ? null : isMeshedTooltip}</React.Fragment>,
        body: ns.name === this.state.selectedNs || ns.name === this.state.defaultOpenNs.name ?
          this.renderNamespaceSection(ns.name) : null
      };
    });

    return (
      <Grid container>

        <Accordion
          onChange={this.onNamespaceChange}
          panels={panelData}
          defaultOpenPanel={_.get(this.state.defaultOpenNs, 'name', null)} />
      </Grid>
    );
  }

  render() {
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <Spinner /> : this.renderAccordion() }
      </div>);
  }
}

export default withContext(NamespaceLanding);
