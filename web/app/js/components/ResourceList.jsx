import 'whatwg-fetch';

import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import withREST from './util/withREST.jsx';

export class ResourceListBase extends React.Component {
  static defaultProps = {
    error: null
  }

  static propTypes = {
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    error:  apiErrorPropType,
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
    const {data, loading, error} = this.props;

    if (loading && !error) {
      return <Spinner />;
    }

    let processedMetrics = [];
    if (data.length === 1) {
      processedMetrics = processSingleResourceRollup(data[0]);
    }

    return (
      <React.Fragment>
        <MetricsTable
          resource={this.props.resource}
          metrics={processedMetrics} />
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

export default withREST(
  ResourceListBase,
  ({api, resource}) => [api.fetchMetrics(api.urlsForResource(resource))],
  {
    resetProps: ['resource'],
  },
);
