import 'whatwg-fetch';

import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import NetworkGraph from './NetworkGraph.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import { Trans } from '@lingui/macro';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import { friendlyTitle } from './util/Utils.js';
import { processMultiResourceRollup } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

class Namespaces extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(props.match.params);
  }

  getInitialState(params) {
    const ns = _get(params, 'namespace', 'default');

    return {
      ns,
      pollingInterval: 2000,
      metrics: {},
      pendingRequests: false,
      loaded: false,
      error: null,
    };
  }

  componentDidMount() {
    this.startServerPolling();
    this.checkNamespaceMatch();
  }

  componentDidUpdate(prevProps) {
    const { match, isPageVisible } = this.props;
    const { params } = match;
    if (!_isEqual(prevProps.match.params.namespace, params.namespace)) {
      // React won't unmount this component when switching resource pages so we need to clear state
      this.api.cancelCurrentRequests();
      this.resetState(params);
    }

    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  resetState(params) {
    this.setState(this.getInitialState(params));
  }

  componentWillUnmount() {
    this.stopServerPolling();
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

  loadFromServer() {
    const { pendingRequests, ns } = this.state;
    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([this.api.fetchMetrics(this.api.urlsForResource('all', ns, true))]);

    Promise.all(this.api.getCurrentPromises())
      .then(([allRollup]) => {
        const metrics = processMultiResourceRollup(allRollup, 'all');

        this.setState({
          metrics,
          loaded: true,
          pendingRequests: false,
          error: null,
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
      error: e,
    });
  }

  checkNamespaceMatch = () => {
    const { ns } = this.state;
    const { selectedNamespace, updateNamespaceInContext } = this.props;

    if (ns !== selectedNamespace) {
      updateNamespaceInContext(ns);
    }
  }

  renderResourceSection = (resource, metrics) => {
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
    const { metrics, ns, loaded, error } = this.state;
    const noMetrics = _isEmpty(metrics.pod);
    const deploymentsWithMetrics = _filter(metrics.deployment, d => d.requestRate > 0);

    return (
      <div className="page-content">
        {!error ? null : <ErrorBanner message={error} />}
        {!loaded ? <Spinner /> : (
          <div>
            {noMetrics ? <div><Trans>noResourcesDetectedMsg</Trans></div> : null}
            {
              _isEmpty(deploymentsWithMetrics) ? null :
              <NetworkGraph namespace={ns} deployments={metrics.deployment} />
            }
            {this.renderResourceSection('deployment', metrics.deployment)}
            {this.renderResourceSection('daemonset', metrics.daemonset)}
            {this.renderResourceSection('pod', metrics.pod)}
            {this.renderResourceSection('replicationcontroller', metrics.replicationcontroller)}
            {this.renderResourceSection('statefulset', metrics.statefulset)}
            {this.renderResourceSection('job', metrics.job)}
            {this.renderResourceSection('trafficsplit', metrics.trafficsplit)}
            {this.renderResourceSection('cronjob', metrics.cronjob)}
            {this.renderResourceSection('replicaset', metrics.replicaset)}

            {
              noMetrics ? null :
              <div className="page-section">
                <MetricsTable
                  title={<Trans>tableTitleTCP</Trans>}
                  resource="pod"
                  metrics={metrics.pod}
                  isTcpTable />
              </div>
            }
          </div>
        )}
      </div>
    );
  }
}

Namespaces.propTypes = {
  api: PropTypes.shape({
    cancelCurrentRequests: PropTypes.func.isRequired,
    fetchMetrics: PropTypes.func.isRequired,
    getCurrentPromises: PropTypes.func.isRequired,
    setCurrentRequests: PropTypes.func.isRequired,
    urlsForResource: PropTypes.func.isRequired,
  }).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  match: PropTypes.shape({
    params: PropTypes.shape({
      namespace: PropTypes.string,
    }),
  }),
  selectedNamespace: PropTypes.string.isRequired,
  updateNamespaceInContext: PropTypes.func.isRequired,
};

Namespaces.defaultProps = {
  match: {
    params: {
      namespace: 'default',
    },
  },
};

export default withPageVisibility(withContext(Namespaces));
