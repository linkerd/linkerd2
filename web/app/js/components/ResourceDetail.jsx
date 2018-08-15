import _ from 'lodash';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import queryString from 'query-string';
import React from 'react';
import { Spin } from 'antd';
import withREST from './util/withREST.jsx';
import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.js';
import './../../css/list.css';
import 'whatwg-fetch';

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
    location: PropTypes.shape({ search: PropTypes.string }).isRequired,
  }

  constructor(props) {
    super(props);
    let query = queryString.parse(props.location.search);

    this.state = this.getInitialState(query);
  }

  getInitialState(params) {
    return {
      namespace: params.namespace,
      resourceName: params.resource_name,
      resourceType: params.resource_type
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
    const {data, loading} = this.props;

    if (loading) {
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
  ({api, location}) => {
    // TODO: handle cases where query is not complete
    let query = queryString.parse(location.search);
    return [api.fetchMetrics(api.urlsForResource(query.resource_type, query.namespace) + "&resource_name=" + query.resource_name)];
  },
  {
    resetProps: ['resource'],
  },
);
