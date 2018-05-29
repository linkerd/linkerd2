import _ from 'lodash';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import { processSingleResourceRollup } from './util/MetricUtils.js';
import React from 'react';
import withREST from './util/withREST.jsx';
import './../../css/list.css';
import 'whatwg-fetch';

export class ResourceList extends React.Component {
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
      return <ConduitSpinner />;
    }

    let processedMetrics = [];
    if (_.has(data, '[0].ok')) {
      processedMetrics = processSingleResourceRollup(data[0]);
    }

    if (_.isEmpty(processedMetrics)) {
      return this.renderEmptyMessage();
    }

    const friendlyTitle = _.startCase(this.props.resource);

    return (
      <MetricsTable
        resource={friendlyTitle}
        metrics={processedMetrics}
        linkifyNsColumn={true} />
    );
  }

  render() {
    const {api, loading, resource} = this.props;

    const friendlyTitle = _.startCase(resource);

    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {loading ? null : <PageHeader header={`${friendlyTitle}s`} api={api} />}
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withREST(
  ResourceList,
  ({api, resource}) => [api.fetchMetrics(api.urlsForResource(resource))],
  ['resource'],
);
