import 'whatwg-fetch';

import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import NetworkGraph from './NetworkGraph.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import { friendlyTitle } from './util/Utils.js';
import { processMultiResourceRollup } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

class Namespaces extends React.Component {
  static defaultProps = {
    match: {
      params: {
        namespace: 'default',
      },
    },
  }

  static propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      fetchMetrics: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
      urlsForResource: PropTypes.func.isRequired,
    }).isRequired,
    match: PropTypes.shape({
      params: PropTypes.shape({
        namespace: PropTypes.string,
      }),
    }),
    selectedNamespace: PropTypes.string.isRequired,
    updateNamespaceInContext: PropTypes.func.isRequired,
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(this.props.match.params);
  }

  getInitialState(params) {
    let ns = _get(params, "namespace", "default");

    return {
      ns: ns,
      pollingInterval: 2000,
      metrics: {},
      pendingRequests: false,
      loaded: false,
      error: null
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.checkNamespaceMatch();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentDidUpdate(prevProps) {
    if (!_isEqual(prevProps.match.params.namespace, this.props.match.params.namespace)) {
      // React won't unmount this component when switching resource pages so we need to clear state
      this.api.cancelCurrentRequests();
      this.setState(this.getInitialState(this.props.match.params));
    }
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

    this.api.setCurrentRequests([this.api.fetchMetrics(this.api.urlsForResource("all", this.state.ns, true))]);

    Promise.all(this.api.getCurrentPromises())
      .then(([allRollup]) => {
        let metrics = processMultiResourceRollup(allRollup, "all");

        this.setState({
          metrics: metrics,
          loaded: true,
          pendingRequests: false,
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

  checkNamespaceMatch = () => {
    if (this.state.ns !== this.props.selectedNamespace) {
      this.props.updateNamespaceInContext(this.state.ns);
    }
  }

  renderResourceSection(resource, metrics) {
    if (_isEmpty(metrics)) {
      return null;
    }
    return (
      <div className="page-section">
        <MetricsTable
          title={friendlyTitle(resource).plural}
          resource={resource}
          metrics={metrics}
          showNamespaceColumn={false} />
      </div>
    );
  }

  render() {
    const { metrics } = this.state;
    let noMetrics = _isEmpty(metrics.pod);
    let deploymentsWithMetrics = _filter(metrics.deployment, d => d.requestRate > 0);

    return (
      <div className="page-content">
        {!this.state.error ? null : <ErrorBanner message={this.state.error} />}
        {!this.state.loaded ? <Spinner /> : (
          <div>
            {noMetrics ? <div>No resources detected.</div> : null}
            {
              _isEmpty(deploymentsWithMetrics) ? null :
              <NetworkGraph namespace={this.state.ns} deployments={metrics.deployment} />
            }
            {this.renderResourceSection("deployment", metrics.deployment)}
            {this.renderResourceSection("daemonset", metrics.daemonset)}
            {this.renderResourceSection("pod", metrics.pod)}
            {this.renderResourceSection("replicationcontroller", metrics.replicationcontroller)}
            {this.renderResourceSection("statefulset", metrics.statefulset)}
            {this.renderResourceSection("job", metrics.job)}
            {this.renderResourceSection("trafficsplit", metrics.trafficsplit)}
            {this.renderResourceSection("cronjob", metrics.cronjob)}
            {this.renderResourceSection("replicaset", metrics.replicaset)}

            {
              noMetrics ? null :
              <div className="page-section">
                <MetricsTable
                  title="TCP"
                  resource="pod"
                  metrics={metrics.pod}
                  isTcpTable={true} />
              </div>
            }
          </div>
        )}
      </div>);
  }
}

export default withContext(Namespaces);
