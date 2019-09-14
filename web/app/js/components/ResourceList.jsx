import 'whatwg-fetch';

import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.jsx';
import { UrlQueryParamTypes, addUrlProps } from 'react-url-query';

import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import withREST from './util/withREST.jsx';
import _mapValues from 'lodash/mapValues';

const topRoutesQueryProps = {
  resource_name: PropTypes.string,
  resource_type: PropTypes.string,
  namespace: PropTypes.string,
  to_name: PropTypes.string,
  to_type: PropTypes.string,
  to_namespace: PropTypes.string,
};
const urlPropsQueryConfig = _mapValues(topRoutesQueryProps, () => {
  return { type: UrlQueryParamTypes.string };
});
export class ResourceListBase extends React.Component {
  static defaultProps = {
    error: null
  }

  static propTypes = {
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    error: apiErrorPropType,
    loading: PropTypes.bool.isRequired,
    nsMetrics: PropTypes.string,
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
      processedMetrics = processSingleResourceRollup(data[0], this.props.resource);
    }

    return (
      <React.Fragment>
        <MetricsTable
          resource={this.props.resource}
          metrics={processedMetrics}
          title="HTTP metrics" />

        {this.props.resource !== "trafficsplit" &&
        <MetricsTable
          resource={this.props.resource}
          isTcpTable={true}
          metrics={processedMetrics}
          title="TCP metrics" />
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

export default addUrlProps({ urlPropsQueryConfig })(withREST(
  ResourceListBase,
  ({ api, resource, nsMetrics }) => [api.fetchMetrics(api.urlsForResource(resource, nsMetrics, true))],
  {
    resetProps: ['resource', 'nsMetrics'],
  },
));
