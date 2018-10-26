import PropTypes from 'prop-types';
import React from 'react';
import classNames from 'classnames';
import { getSuccessRateClassification } from './MetricUtils.jsx';
import { statusClassNames } from './theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => statusClassNames(theme);

class SuccessRateDot extends React.Component {
  render() {
    const { sr, classes } = this.props;

    return (
      <div className={classNames("success-rate-dot", classes[getSuccessRateClassification(sr)])} />
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
