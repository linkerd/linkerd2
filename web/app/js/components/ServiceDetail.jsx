import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import TopRoutesTabs from './TopRoutesTabs.jsx';
import _isEmpty from 'lodash/isEmpty';

const ServiceDetail = ({
  api,
  resourceMetrics,
  resourceName,
  query,
  isTcpOnly,
  pathPrefix,
  upstreamDisplayMetrics,
  downstreamDisplayMetrics,
  unmeshedSources,
  updateUnmeshedSources,
  upstreams,
}) => {
  return (
    <div>
      <Grid container justifyContent="space-between" alignItems="center">
        <Grid item><Typography variant="h5">service/{resourceName}</Typography></Grid>
      </Grid>

      <Octopus
        resource={resourceMetrics[0]}
        neighbors={{ upstream: upstreamDisplayMetrics, downstream: downstreamDisplayMetrics }}
        unmeshedSources={Object.values(unmeshedSources)}
        api={api} />

      {isTcpOnly ? null : <TopRoutesTabs
        query={query}
        pathPrefix={pathPrefix}
        updateUnmeshedSources={updateUnmeshedSources}
        disableTop="false" />
      }

      {_isEmpty(upstreams) ? null :
      <MetricsTable
        resource="multi_resource"
        title={<Trans>tableTitleInbound</Trans>}
        metrics={upstreamDisplayMetrics} />
      }

      {_isEmpty(downstreamDisplayMetrics) ? null :
      <MetricsTable
        resource="multi_resource"
        title={<Trans>tableTitleOutbound</Trans>}
        metrics={downstreamDisplayMetrics} />
      }

    </div>
  );
};

ServiceDetail.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  resourceMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  resourceName: PropTypes.string.isRequired,
  query: PropTypes.string.isRequired,
  isTcpOnly: PropTypes.bool.isRequired,
  pathPrefix: PropTypes.string.isRequired,
  upstreamDisplayMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  downstreamDisplayMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  unmeshedSources: PropTypes.shape({}).isRequired,
  updateUnmeshedSources: PropTypes.func.isRequired,
  upstreams: PropTypes.shape({}).isRequired,
};

export default ServiceDetail;
