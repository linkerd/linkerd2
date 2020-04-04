import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { jaegerIcon } from './util/SvgWrappers.jsx';

const JaegerLink = ({ PrefixedLink, name, namespace, resource }) => {
  const link = `/jaeger/linkerd-${resource}?var-${resource}=${name}`;
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
