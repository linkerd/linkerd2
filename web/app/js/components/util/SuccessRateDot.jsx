import PropTypes from 'prop-types';
import React from 'react';
import _merge from 'lodash/merge';
import classNames from 'classnames';
import { getSuccessRateClassification } from './MetricUtils.jsx';
import { statusClassNames } from './theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => _merge({}, statusClassNames(theme), {
  successRateDot: {
    width: theme.spacing(1),
    height: theme.spacing(1),
    minWidth: theme.spacing(1),
    borderRadius: "50%"
  }
});

const SuccessRateDot = ({ sr, classes }) => (
  <div className={classNames(classes.successRateDot, classes[getSuccessRateClassification(sr)])} />
);

SuccessRateDot.propTypes = {
  sr: PropTypes.number,
};

SuccessRateDot.defaultProps = {
  sr: null
};

export default withStyles(styles, { withTheme: true })(SuccessRateDot);
