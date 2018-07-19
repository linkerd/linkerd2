import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import { friendlyTitle } from './util/Utils.js';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Spin } from 'antd';
import withREST from './util/withREST.jsx';
import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.js';
import './../../css/list.css';
import 'whatwg-fetch';

export class ResourceListBase extends React.Component {
  static defaultProps = {
    error: null,
  }

  static propTypes = {
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    error: PropTypes.oneOfType([PropTypes.string, PropTypes.instanceOf(Error)]),
    loading: PropTypes.bool.isRequired,
    resource: PropTypes.string.isRequired,
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
        resource={friendlyTitle(this.props.resource).singular}
        metrics={processedMetrics} />
    );
  }

  render() {
    const {loading, resource} = this.props;

    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {loading ? null : <PageHeader header={friendlyTitle(resource).plural} />}
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withREST(
  ResourceListBase,
  ({api, resource}) => [api.fetchMetrics(api.urlsForResource(resource))],
  {
    resetProps: ['resource'],
  },
);
