import _ from 'lodash';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
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
      return <ConduitSpinner />;
    }

    let processedMetrics = [];
    if (_.has(data, '[0].ok')) {
      processedMetrics = processSingleResourceRollup(data[0]);
    }

    const friendlyTitle = _.startCase(this.props.resource);

    return (
      <MetricsTable
        resource={friendlyTitle}
        metrics={processedMetrics} />
    );
  }

  render() {
    const {loading, resource} = this.props;

    const friendlyTitle = _.startCase(resource);

    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {loading ? null : <PageHeader header={`${friendlyTitle}s`} />}
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withREST(
  ResourceListBase,
  ({api, resource}) => [api.fetchMetrics(api.urlsForResource(resource))],
  ['resource'],
);
