import PropTypes from 'prop-types';
import React from 'react';

const TapLink = ({PrefixedLink, namespace, resource, toNamespace, toResource, path, disabled}) => {
  if (disabled || namespace === "") {
    return <i className="fas fa-microscope tapGrayed" />;
  }
  let params = {
    autostart: "true",
    namespace,
    resource,
    toNamespace,
    toResource,
    path
  };
  let queryStr = Object.entries(params).map(([k, v]) => `${k}=${v}`).join("&");

  return (
    <PrefixedLink to={`/tap?${queryStr}`}>
      <i className="fas fa-microscope" />
    </PrefixedLink>
  );
};

TapLink.propTypes = {
  disabled: PropTypes.bool,
  namespace: PropTypes.string,
  path: PropTypes.string.isRequired,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
  toNamespace: PropTypes.string.isRequired,
  toResource: PropTypes.string.isRequired,
};

TapLink.defaultProps = {
  disabled: false,
  namespace: ""
};

export default TapLink;
