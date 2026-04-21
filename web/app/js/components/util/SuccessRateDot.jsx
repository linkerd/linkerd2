import PropTypes from 'prop-types';
import React from 'react';
import _merge from 'lodash/merge';
import classNames from 'classnames';
import { withStyles } from '@material-ui/core/styles';
import { getSuccessRateClassification } from './MetricUtils.jsx';
import { statusClassNames } from './theme.js';

const styles = theme => _merge({}, statusClassNames(theme), {
  successRateDot: {
    width: theme.spacing(1),
    height: theme.spacing(1),
    minWidth: theme.spacing(1),
    borderRadius: '50%',
  },
});

const SuccessRateDot = function({ sr, classes }) {
  return <div className={classNames(classes.successRateDot, classes[getSuccessRateClassification(sr)])} />;
};

SuccessRateDot.propTypes = {
  sr: PropTypes.number,
};

SuccessRateDot.defaultProps = {
  sr: null,
};

export default withStyles(styles, { withTheme: true })(SuccessRateDot);
