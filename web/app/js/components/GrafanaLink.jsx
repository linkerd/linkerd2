import PropTypes from 'prop-types';
import React from 'react';
import { grafanaIcon } from './util/SvgWrappers.jsx';

const GrafanaLink = ({PrefixedLink, name, namespace, resource}) => {
  return (
    <PrefixedLink
      to={`/dashboard/db/linkerd-${resource}?var-namespace=${namespace}&var-${resource}=${name}`}
      deployment="linkerd-grafana"
      targetBlank={true}>
      &nbsp;&nbsp;
      {grafanaIcon}
    </PrefixedLink>
  );
};

GrafanaLink.propTypes = {
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string.isRequired,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

export default GrafanaLink;
