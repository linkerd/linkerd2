import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import PropTypes from 'prop-types';
import React from 'react';
import { faMicroscope } from '@fortawesome/free-solid-svg-icons/faMicroscope';
import { Link } from 'react-router-dom';

const TapLink = function({ namespace, resource, toNamespace, toResource, path, disabled }) {
  if (disabled || namespace === '') {
    return <FontAwesomeIcon icon={faMicroscope} className="tapGrayed" />;
  }
  const params = {
    autostart: 'true',
    namespace,
    resource,
    toNamespace,
    toResource,
    path,
  };
  const queryStr = Object.entries(params).map(([k, v]) => `${k}=${v}`).join('&');

  return (
    <Link to={`/tap?${queryStr}`}>
      <FontAwesomeIcon icon={faMicroscope} />
    </Link>
  );
};

TapLink.propTypes = {
  disabled: PropTypes.bool,
  namespace: PropTypes.string,
  path: PropTypes.string.isRequired,
  resource: PropTypes.string.isRequired,
  toNamespace: PropTypes.string.isRequired,
  toResource: PropTypes.string.isRequired,
};

TapLink.defaultProps = {
  disabled: false,
  namespace: '',
};

export default TapLink;
