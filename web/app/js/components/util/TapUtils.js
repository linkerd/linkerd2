import PropTypes from 'prop-types';

export const httpMethods = ["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"];

export const defaultMaxRps = "1.0";

export const tapQueryPropType = PropTypes.shape({
  resource: PropTypes.string,
  namespace: PropTypes.string,
  toResource: PropTypes.string,
  toNamespace: PropTypes.string,
  method: PropTypes.string,
  path: PropTypes.string,
  scheme: PropTypes.string,
  authority: PropTypes.string,
  maxRps: PropTypes.string
});
