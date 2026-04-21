import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _isEmpty from 'lodash/isEmpty';
import _filter from 'lodash/filter';
import TopRoutesTabs, { topRoutesQueryPropType } from './TopRoutesTabs.jsx';
import Octopus from './Octopus.jsx';
import MetricsTable from './MetricsTable.jsx';

const getResourceForService = (resourceMetrics, serviceName) => {
  if (resourceMetrics.length === 1) {
    return resourceMetrics[0];
  }
  const relevantResources = _filter(resourceMetrics, rm => {
    return !rm.tsStats || rm.tsStats.leaf === serviceName;
  });

  if (relevantResources.length >= 1) {
    return relevantResources[0];
  }

  return resourceMetrics[0];
};

const ServiceDetail = function({
  api,
  resourceMetrics,
  resourceName,
  query,
  isTcpOnly,
  pathPrefix,
  upstreamDisplayMetrics,
  unmeshedSources,
  updateUnmeshedSources,
  upstreams,
}) {
  return (
    <div>
      <Grid container justifyContent="space-between" alignItems="center">
        <Grid item><Typography variant="h5">service/{resourceName}</Typography></Grid>
      </Grid>

      <Octopus
        resource={getResourceForService(resourceMetrics)}
        neighbors={{ upstream: upstreamDisplayMetrics }}
        unmeshedSources={Object.values(unmeshedSources)}
        api={api} />

      {isTcpOnly ? null : <TopRoutesTabs
        query={query}
        pathPrefix={pathPrefix}
        updateUnmeshedSources={updateUnmeshedSources}
        disableTop />
      }

      {_isEmpty(upstreams) ? null :
      <MetricsTable
        resource="multi_resource"
        title={<Trans>tableTitleInbound</Trans>}
        metrics={upstreamDisplayMetrics} />
      }

      {resourceMetrics.length <= 1 ? null :
      <MetricsTable
        resource="service"
        title={<Trans>tableTitleOutbound</Trans>}
        metrics={resourceMetrics} />
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
  query: topRoutesQueryPropType.isRequired,
  isTcpOnly: PropTypes.bool.isRequired,
  pathPrefix: PropTypes.string.isRequired,
  upstreamDisplayMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  unmeshedSources: PropTypes.shape({}).isRequired,
  updateUnmeshedSources: PropTypes.func.isRequired,
  upstreams: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
};

export default ServiceDetail;
