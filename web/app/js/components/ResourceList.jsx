import _ from 'lodash';
import CallToAction from './CallToAction.jsx';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import { processSingleResourceRollup } from './util/MetricUtils.js';
import React from 'react';
import './../../css/list.css';
import 'whatwg-fetch';

export default class ResourceList extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = this.getInitialState(this.props);
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
    this.startServerPolling(this.props.resource);
  }

  componentWillReceiveProps(newProps) {
    // React won't unmount this component when switching resource pages so we need to clear state
    this.stopServerPolling();
    this.setState(this.getInitialState(newProps));
    this.startServerPolling(newProps.resource);
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  startServerPolling(resource) {
    this.loadFromServer(resource);
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval, resource);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  loadFromServer(resource) {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource(resource))
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([rollup]) => {
        let processedMetrics = processSingleResourceRollup(rollup, this.props.controllerNamespace);

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

  renderEmptyMessage() {
    let shortResource = this.props.resource === "replication_controller" ?
      "RC" : this.props.resource;
    return (<CallToAction
      resource={shortResource}
      numResources={_.size(this.state.metrics)} />);
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
              this.renderEmptyMessage(friendlyTitle) :
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
