import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import { processSingleResourceRollup } from './util/MetricUtils.js';
import PropTypes from 'prop-types';
import React from 'react';
import { singularResource } from './util/Utils.js';
import { Spin } from 'antd';
import { withContext } from './util/AppContext.jsx';
import 'whatwg-fetch';

const getResourceFromUrl = (match, pathPrefix) => {
  let resource = {
    namespace: match.params.namespace
  };
  let regExp = RegExp(`${pathPrefix || ""}/namespaces/${match.params.namespace}/([^/]+)/([^/]+)`);
  let urlParts = match.url.match(regExp);

  resource.type = singularResource(urlParts[1]);
  resource.name = urlParts[2];

  if (match.params[resource.type] !== resource.name) {
    console.error("Failed to extract resource from URL");
  }
  return resource;
};

export class ResourceDetailBase extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    match: PropTypes.shape({}).isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(props.match, props.pathPrefix);
  }

  getInitialState(match, pathPrefix) {
    let resource = getResourceFromUrl(match, pathPrefix);
    return {
      namespace: resource.namespace,
      resourceName: resource.name,
      resourceType: resource.type,
      resource,
      pollingInterval: 2000,
      resourceMetrics: [],
      podMetrics: [],
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
    this.setState(this.getInitialState(newProps.match, newProps.pathPrefix));
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

    let { resource } = this.state;
    this.api.setCurrentRequests([
      // inbound stats for this resource
      this.api.fetchMetrics(
        `${this.api.urlsForResource(resource.type, resource.namespace)}&resource_name=${resource.name}`
      ),
      // pod metrics for outbound stats --to this resource
      // only gets traffic originating within the mesh
      this.api.fetchMetrics(
        `${this.api.urlsForResource("pod", resource.namespace)}&to_name=${resource.name}&to_type=${resource.type}`
      ),
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([resourceRsp, podRsp]) => {
        let resourceMetrics = processSingleResourceRollup(resourceRsp);
        let podMetrics = processSingleResourceRollup(podRsp);

        this.setState({
          resourceMetrics,
          podMetrics,
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
      loaded: true,
      pendingRequests: false,
      error: e
    });
  }

  banner = () => {
    if (!this.state.error) {
      return;
    }

    return <ErrorBanner message={this.state.error} />;
  }

  content = () => {
    if (!this.state.loaded && !this.state.error) {
      return <Spin size="large" />;
    }

    return (
      <div>
        <div className="page-section">
          <MetricsTable
            resource={this.state.resource.type}
            metrics={this.state.resourceMetrics} />
        </div>

        <div className="page-section">
          <h2 className="subsection-header">Pods</h2>
          <MetricsTable
            resource="pod"
            metrics={this.state.podMetrics} />
        </div>
      </div>
    );
  }

  render() {
    let resourceBreadcrumb = (
      <React.Fragment>
        <this.api.PrefixedLink to={"/namespaces/" + this.state.namespace}>
          {this.state.namespace}
        </this.api.PrefixedLink> &gt; {`${this.state.resource.type}/${this.state.resource.name}`}
      </React.Fragment>
    );

    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {resourceBreadcrumb}
          <PageHeader header={`${this.state.resource.type}/${this.state.resource.name}`} />
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withContext(ResourceDetailBase);
