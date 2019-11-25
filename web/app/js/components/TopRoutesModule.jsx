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
import { withTranslation } from 'react-i18next';

class TopRoutesBase extends React.Component {
  static defaultProps = {
    error: null
  }

  static propTypes = {
    data: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
    error:  apiErrorPropType,
    loading: PropTypes.bool.isRequired,
    query: PropTypes.shape({}).isRequired,
    t: PropTypes.func.isRequired,
  }

  banner = () => {
    const {error} = this.props;
    if (!error) {
      return;
    }
    return <ErrorBanner message={error} />;
  }

  loading = () => {
    const {loading} = this.props;
    if (!loading) {
      return;
    }

    return <Spinner />;
  }

  render() {
    const {data, loading, t} = this.props;
    let results = _get(data, '[0].ok.routes', []);
    results = _sortBy(results, o => o.resource);

    let metricsByResource = results.map(r => {
      return {
        resource: r.resource,
        rows: processTopRoutesResults(r.rows, t)
      };
    });

    return (
      <React.Fragment>
        {this.loading()}
        {this.banner()}
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
  }
}

export default withTranslation(["topRoutes"])(withREST(
  TopRoutesBase,
  ({api, query}) => {
    let queryParams = new URLSearchParams(query).toString();
    return [api.fetchMetrics(`/api/routes?${queryParams}`)];
  }
));
