import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { jaegerIcon } from './util/SvgWrappers.jsx';

function jaegerQuery(name, namespace, resource) {
  if (_isEmpty(namespace)) {
    return `{"linkerd.io/workload-ns"%3A"${name}"}`;
  } else if (resource === 'pod') {
    return `{"hostname"%3A"${name}"%2C"linkerd.io/workload-ns"%3A"${namespace}"}`;
  } else {
    return `{"linkerd.io%2Fproxy-${resource}"%3A"${name}"%2C"linkerd.io/workload-ns"%3A"${namespace}"}`;
  }
}

const JaegerLink = ({ PrefixedLink, name, namespace, resource }) => {
  const link = `/jaeger/search?service=linkerd-proxy&tags=${jaegerQuery(name, namespace, resource)}`;

  return (
    <PrefixedLink
      to={link}
      targetBlank>
      &nbsp;&nbsp;
      {jaegerIcon}
    </PrefixedLink>
  );
};

JaegerLink.propTypes = {
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

JaegerLink.defaultProps = {
  namespace: '',
};

export default JaegerLink;
