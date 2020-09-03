import 'whatwg-fetch';

import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import { Trans } from '@lingui/macro';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import withREST from './util/withREST.jsx';

export class ResourceListBase extends React.Component {
  banner = () => {
    const { error } = this.props;
    return error ? <ErrorBanner message={error} /> : null;
  }

  content = () => {
    const { data, loading, error, resource } = this.props;

    if (loading && !error) {
      return <Spinner />;
    }

    let processedMetrics = [];
    if (data.length === 1) {
      processedMetrics = processSingleResourceRollup(data[0], resource);
    }

    return (
      <React.Fragment>
        <MetricsTable
          resource={resource}
          metrics={processedMetrics}
          title={<Trans>tableTitleHTTPMetrics</Trans>} />

        {resource !== 'trafficsplit' &&
        <MetricsTable
          resource={resource}
          isTcpTable
          metrics={processedMetrics}
          title={<Trans>tableTitleTCPMetrics</Trans>} />
        }
      </React.Fragment>
    );
  }

  render() {
    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {this.content()}
        </div>
      </div>
    );
  }
}

ResourceListBase.propTypes = {
  data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
  error: apiErrorPropType,
  loading: PropTypes.bool.isRequired,
  resource: PropTypes.string.isRequired,
};

ResourceListBase.defaultProps = {
  error: null,
};

// When constructing a ResourceList for type "namespace", we query the API for metrics for all namespaces. For all other resource types, we limit our API query to the selectedNamespace.
export default withREST(
  ResourceListBase,
  ({ api, resource, selectedNamespace }) => [api.fetchMetrics(api.urlsForResource(resource, resource === 'namespace' ? 'all' : selectedNamespace, true))],
  {
    resetProps: ['resource', 'selectedNamespace'],
  },
);
