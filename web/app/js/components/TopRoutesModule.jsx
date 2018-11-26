import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TopRoutesTable from './TopRoutesTable.jsx';
import Typography from '@material-ui/core/Typography';
import _ from 'lodash';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import { processTopRoutesResults } from './util/MetricUtils.jsx';
import withREST from './util/withREST.jsx';

class TopRoutesBase extends React.Component {
  static defaultProps = {
    error: null
  }

  static propTypes = {
    data: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
    error:  apiErrorPropType,
    query: PropTypes.shape({}).isRequired,
  }

  banner = () => {
    const {error} = this.props;
    if (!error) {
      return;
    }
    return <ErrorBanner message={error} />;
  }

  configureProfileMsg() {
    return (
      <Card>
        <CardContent>
          <Typography>
            No traffic found.  Does the service have a service profile?  You can create one with the `linkerd profile` command.
          </Typography>
        </CardContent>
      </Card>
    );
  }

  render() {
    const {data} = this.props;
    let metrics = processTopRoutesResults(_.get(data, '[0].routes.rows', []));

    return (
      <React.Fragment>
        {this.banner()}
        {_.isEmpty(metrics) ? this.configureProfileMsg() : null}
        <TopRoutesTable rows={metrics} />
      </React.Fragment>
    );
  }
}

export default withREST(
  TopRoutesBase,
  ({api, query}) => {
    let queryParams = new URLSearchParams(query).toString();
    return [api.fetchMetrics(`/api/routes?${queryParams}`)];
  }
);
