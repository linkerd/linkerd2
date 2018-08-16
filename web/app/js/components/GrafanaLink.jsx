import grafanaIcon from './../../img/grafana.svg';
import PropTypes from 'prop-types';
import React from 'react';

const GrafanaLink = ({PrefixedLink, name, namespace, resource}) => {
  const iconSize = 15;
  return (
    <PrefixedLink
      to={`/dashboard/db/linkerd-${resource}?var-namespace=${namespace}&var-${resource}=${name}`}
      deployment="grafana"
      targetBlank={true}>
      &nbsp;&nbsp;
      <img
        src={grafanaIcon}
        onError={e => {
          // awful hack to deal with the fact that we don't serve assets off absolute paths
          e.target.src = e.target.src.replace(/(.*)(\/[a-zA-Z]*)(\/dist)(.*)/, "$1$3$4");
        }}
        width={iconSize}
        height={iconSize}
        title={`${resource} grafana dashboard`}
        alt={`link to ${resource} grafana dashboard`} />
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
