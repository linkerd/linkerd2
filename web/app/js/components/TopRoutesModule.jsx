import CardContent from '@material-ui/core/CardContent';
import ConfigureProfilesMsg from './ConfigureProfilesMsg.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import TopRoutesTable from './TopRoutesTable.jsx';
import Typography from '@material-ui/core/Typography';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _sortBy from 'lodash/sortBy';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import { processTopRoutesResults } from './util/MetricUtils.jsx';
import withREST from './util/withREST.jsx';

const TopRoutesBase = ({ data, loading, error }) => {
  let results = _get(data, '[0].ok.routes', []);
  results = _sortBy(results, o => o.resource);

  const metricsByResource = results.map(r => {
    return {
      resource: r.resource,
      rows: processTopRoutesResults(r.rows),
    };
  });

  return (
    <React.Fragment>
      {loading ? <Spinner /> : null}
      {error ? <ErrorBanner message={error} /> : null}
      {
        metricsByResource.map(metric => {
          return (
            <CardContent key={metric.resource}>
              <Typography variant="h5">{metric.resource}</Typography>
              <TopRoutesTable rows={metric.rows} />
            </CardContent>
          );
        })
      }
      { !loading && _isEmpty(metricsByResource) ? <ConfigureProfilesMsg /> : null }
    </React.Fragment>
  );
};

TopRoutesBase.defaultProps = {
  error: null,
};

TopRoutesBase.propTypes = {
  data: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  error: apiErrorPropType,
  loading: PropTypes.bool.isRequired,
  query: PropTypes.shape({}).isRequired,
};

export default withREST(
  TopRoutesBase,
  ({ api, query }) => {
    const queryParams = new URLSearchParams(query).toString();
    return [api.fetchMetrics(`/api/routes?${queryParams}`)];
  },
);
