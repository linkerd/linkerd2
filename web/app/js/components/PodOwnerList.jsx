import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import { processRollupMetrics } from './util/MetricUtils.js';
import React from 'react';
import './../../css/list.css';
import 'whatwg-fetch';

export default class PodOwnerList extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      pollingInterval: 2000, // TODO: poll based on metricsWindow size
      metrics: [],
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillReceiveProps() {
    // React won't unmount this component when switching between Deployments and
    // Replication Controllers, so we need to clear state
    this.api.cancelCurrentRequests();
    this.setState(this.getInitialState());
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

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource[this.props.resource].url().rollup)
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([rollup]) => {
        let processedMetrics = processRollupMetrics(rollup, this.props.controllerNamespace);
        this.setState({
          metrics: processedMetrics,
          loaded: true,
          pendingRequests: false,
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

  render() {
    let friendlyTitle = _.startCase(this.props.resource);
    return (
      <div className="page-content">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner />  :
          <div>
            <PageHeader header={friendlyTitle + "s"} api={this.api} />
            { _.isEmpty(this.state.metrics) ?
              <CallToAction numDeployments={_.size(this.state.metrics)} /> :
              <MetricsTable
                resource={friendlyTitle}
                metrics={this.state.metrics}
                api={this.api} />
            }
          </div>
        }
      </div>);
  }
}
