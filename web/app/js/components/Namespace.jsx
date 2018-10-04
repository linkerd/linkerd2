import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import { friendlyTitle } from './util/Utils.js';
import MetricsTable from './MetricsTable.jsx';
import NetworkGraph from './NetworkGraph.jsx';
import { processMultiResourceRollup } from './util/MetricUtils.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import { withContext } from './util/AppContext.jsx';
import 'whatwg-fetch';

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
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(this.props.match.params);
  }

  getInitialState(params) {
    let ns = _.get(params, "namespace", "default");

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
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillReceiveProps(newProps) {
    // React won't unmount this component when switching resource pages so we need to clear state
    this.api.cancelCurrentRequests();
    this.setState(this.getInitialState(newProps.match.params));
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

    this.api.setCurrentRequests([this.api.fetchMetrics(this.api.urlsForResource("all", this.state.ns))]);

    Promise.all(this.api.getCurrentPromises())
      .then(([allRollup]) => {
        let metrics = processMultiResourceRollup(allRollup);

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

  renderResourceSection(resource, metrics) {
    if (_.isEmpty(metrics)) {
      return null;
    }
    return (
      <div className="page-section">
        <h1>{friendlyTitle(resource).plural}</h1>
        <MetricsTable
          resource={resource}
          metrics={metrics}
          showNamespaceColumn={false} />
      </div>
    );
  }

  render() {
    const {metrics} = this.state;
    let noMetrics = _.isEmpty(metrics.pod);
    let deploymentsWithMetrics = _.filter(metrics.deployment, "requestRate");

    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <Spinner /> : (
          <div>
            { noMetrics ? <div>No resources detected.</div> : null}
            {
              _.isEmpty(deploymentsWithMetrics) ? null :
              <NetworkGraph namespace={this.state.ns} deployments={metrics.deployment} />
            }
            {this.renderResourceSection("deployment", metrics.deployment)}
            {this.renderResourceSection("replicationcontroller", metrics.replicationcontroller)}
            {this.renderResourceSection("pod", metrics.pod)}
            {this.renderResourceSection("authority", metrics.authority)}
          </div>
        )}
      </div>);
  }
}

export default withContext(Namespaces);
