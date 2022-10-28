import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { jaegerIcon } from './util/SvgWrappers.jsx';
import { Link } from 'react-router-dom';

function jaegerQuery(name, namespace, resource) {
  if (_isEmpty(namespace)) {
    return `{"linkerd.io/workload-ns"%3A"${name}"}`;
  } else if (resource === 'pod') {
    return `{"hostname"%3A"${name}"%2C"linkerd.io/workload-ns"%3A"${namespace}"}`;
  } else {
    return `{"linkerd.io%2Fproxy-${resource}"%3A"${name}"%2C"linkerd.io/workload-ns"%3A"${namespace}"}`;
  }
}

const JaegerLink = function({ name, namespace, resource }) {
  const link = `/jaeger/search?service=linkerd-proxy&tags=${jaegerQuery(name, namespace, resource)}`;

  return (
    <Link
      to={link}
      targetBlank>
      &nbsp;&nbsp;
      {jaegerIcon}
    </Link>
  );
};

JaegerLink.propTypes = {
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  resource: PropTypes.string.isRequired,
};

JaegerLink.defaultProps = {
  namespace: '',
};

export default JaegerLink;
