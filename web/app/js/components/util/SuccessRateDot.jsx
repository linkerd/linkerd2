import PropTypes from 'prop-types';
import React from 'react';
import _merge from 'lodash/merge';
import classNames from 'classnames';
import { getSuccessRateClassification } from './MetricUtils.jsx';
import { statusClassNames } from './theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => _merge({}, statusClassNames(theme), {
  successRateDot: {
    width: theme.spacing.unit,
    height: theme.spacing.unit,
    minWidth: theme.spacing.unit,
    borderRadius: "50%"
  }
});

class SuccessRateDot extends React.Component {
  render() {
    const { sr, classes } = this.props;

    return (
      <div className={classNames(classes.successRateDot, classes[getSuccessRateClassification(sr)])} />
    );
  }
}

SuccessRateDot.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  sr: PropTypes.number,
};

SuccessRateDot.defaultProps = {
  sr: null
};

export default withStyles(styles, { withTheme: true })(SuccessRateDot);
