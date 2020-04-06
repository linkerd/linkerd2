import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { jaegerIcon } from './util/SvgWrappers.jsx';

const JaegerLink = ({ PrefixedLink, name, namespace, resource }) => {
  let link = '/jaeger/search?service=linkerd-proxy&tags=';
  if (_isEmpty(namespace)) {
    link += `{"namespace"%3A"${name}"}`;
  } else if (resource === 'pod') {
    link += `{"hostname"%3A"${name}"%2C"namespace"%3A"${namespace}"}`;
  } else {
    link += `{"linkerd.io%2Fproxy-${resource}"%3A"${name}"%2C"namespace"%3A"${namespace}"}`;
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
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

JaegerLink.defaultProps = {
  namespace: '',
};

export default JaegerLink;
