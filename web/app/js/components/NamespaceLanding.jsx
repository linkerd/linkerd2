import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import { friendlyTitle } from './util/Utils.js';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { Collapse, Icon, Tooltip } from 'antd';
import Spinner from './util/Spinner.jsx';
import { processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';
import 'whatwg-fetch';

const isMeshedTooltip = (
  <Tooltip placement="right" title="Namespace is meshed" overlayStyle={{ fontSize: "12px" }}>
    <Icon className="status-ok" type="check-circle" />
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

  onNsSelect = ns => {
    this.setState({
      selectedNs: ns
    });
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

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
      <div className="page-section">
        <h3>{friendlyTitle(resource).plural}</h3>
        <MetricsTable
          resource={resource}
          metrics={metrics}
          showNamespaceColumn={false} />
      </div>
    );
  }

  renderNamespaceSection(namespace) {
    if (!_.has(this.state.metricsByNs, namespace)) {
      return <Spinner />;
    }

    let metrics = this.state.metricsByNs[namespace] || {};
    let noMetrics = _.isEmpty(metrics.pod);

    return (
      <div>
        <h2>Namespace: {namespace}</h2>
        { noMetrics ? <div>No resources detected.</div> : null}
        {this.renderResourceSection("deployment", metrics.deployment)}
        {this.renderResourceSection("replicationcontroller", metrics.replicationcontroller)}
        {this.renderResourceSection("pod", metrics.pod)}
        {this.renderResourceSection("authority", metrics.authority)}
      </div>
    );
  }

  renderAccordion() {
    return (
      <React.Fragment>
        <PageHeader header="Namespaces" />
        <Collapse
          accordion={true}
          defaultActiveKey={_.get(this.state.defaultOpenNs, 'name', null)}
          onChange={this.onNsSelect}>
          {
            _.map(this.state.namespaces, ns => {
              let header = (
                <React.Fragment>{ns.name} {!ns.added ? null : isMeshedTooltip}</React.Fragment>
              );

              return (
                <Collapse.Panel
                  showArrow={false}
                  header={header}
                  key={ns.name}>
                  {ns.name === this.state.selectedNs || ns.name === this.state.defaultOpenNs.name ?
                  this.renderNamespaceSection(ns.name) : null}
                </Collapse.Panel>
              );
            })
          }
        </Collapse>
      </React.Fragment>
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
