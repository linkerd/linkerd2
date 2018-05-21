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
  render() {
    const {api, data, error, loading, resource} = this.props;

    if (error) return  <ErrorBanner message={error} />;
    if (loading) return <ConduitSpinner />;

    const processedMetrics = processSingleResourceRollup(data[0]);

    const friendlyTitle = _.startCase(resource);
    return (
      <div className="page-content">
        <div>
          <PageHeader header={`${friendlyTitle}s`} api={api} />
          { _.isEmpty(processedMetrics) ?
            this.renderEmptyMessage(processedMetrics) :
            <MetricsTable
              resource={friendlyTitle}
              metrics={processedMetrics}
              linkifyNsColumn={true} />
          }
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
