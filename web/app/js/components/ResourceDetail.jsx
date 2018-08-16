import _ from 'lodash';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { singularResource } from './util/Utils.js';
import { Spin } from 'antd';
import withREST from './util/withREST.jsx';
import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.js';
import './../../css/list.css';
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
  static defaultProps = {
    error: null
  }

  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    error:  apiErrorPropType,
    loading: PropTypes.bool.isRequired,
    match: PropTypes.shape({}).isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.state = this.getInitialState(props.match, props.pathPrefix);
  }

  getInitialState(match) {
    let resource = getResourceFromUrl(match);
    return {
      namespace: resource.namespace,
      resourceName: resource.name,
      resourceType: resource.type
    };
  }

  banner = () => {
    const {error} = this.props;

    if (!error) {
      return;
    }

    return <ErrorBanner message={error} />;
  }

  content = () => {
    const {data, loading, error} = this.props;

    if (loading && !error) {
      return <Spin size="large" />;
    }

    let processedMetrics = [];
    if (_.has(data, '[0].ok')) {
      processedMetrics = processSingleResourceRollup(data[0]);
    }

    return (
      <MetricsTable
        resource={this.state.resourceType}
        metrics={processedMetrics} />
    );
  }

  render() {
    const {loading, api} = this.props;
    let resourceBreadcrumb = (
      <React.Fragment>
        <api.PrefixedLink to={"/namespaces/" + this.state.namespace}>{this.state.namespace}</api.PrefixedLink> &gt; {`${this.state.resourceType}/${this.state.resourceName}`}
      </React.Fragment>
    );

    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {loading ? null : <PageHeader header={`${this.state.resourceType}/${this.state.resourceName}`} />}
          {resourceBreadcrumb}
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withREST(
  ResourceDetailBase,
  ({api, match, pathPrefix}) => {
    let resource = getResourceFromUrl(match, pathPrefix);
    return [api.fetchMetrics(api.urlsForResource(resource.type, resource.namespace) + "&resource_name=" + resource.name)];
  },
  {
    resetProps: ['resource'],
  },
);
