import PropTypes from 'prop-types';
import React from 'react';
import _ from 'lodash';

const TapLink = ({PrefixedLink, namespace, resource, toNamespace, toResource, path}) => {
  let params = {
    autostart: "true",
    namespace,
    resource,
    toNamespace,
    toResource,
    path
  };
  let queryStr = _.join(_.map(params, (v,k) => `${k}=${v}`), "&");

  return (
    <PrefixedLink to={`/tap?${queryStr}`}>
      <i className="fas fa-microscope" />
    </PrefixedLink>
  );
};

TapLink.propTypes = {
  namespace: PropTypes.string.isRequired,
  path: PropTypes.string.isRequired,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
  toNamespace: PropTypes.string.isRequired,
  toResource: PropTypes.string.isRequired,
};

export default TapLink;
