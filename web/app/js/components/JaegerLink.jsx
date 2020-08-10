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

const JaegerLink = ({ addr, PrefixedLink, redirect, name, namespace, resource }) => {
  let link = 'jaeger';

  if (redirect === 'true') {
    link = addr;
  }

  link += `/search?service=linkerd-proxy&tags=${jaegerQuery(name, namespace, resource)}`;


  if (redirect === 'true') {
    return (
      <a
        href={link}
        targetBlank>
        &nbsp;&nbsp;
        {jaegerIcon}
      </a>
    );
  }

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
  addr: PropTypes.string,
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  redirect: PropTypes.string.isRequired,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

JaegerLink.defaultProps = {
  addr: '',
  namespace: '',
};

export default JaegerLink;
