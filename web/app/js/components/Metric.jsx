import PropTypes from 'prop-types';
import React from 'react';

const Metric = ({className, title, value}) => (
  <div className={`metric ${className || ""}`}>
    <div className="metric-title">{title}</div>
    <div className="metric-value">{value}</div>
  </div>
);

Metric.propTypes = {
  className: PropTypes.string.isRequired,
  title: PropTypes.string.isRequired,
  value: PropTypes.number.isRequired,
};

export default Metric;
