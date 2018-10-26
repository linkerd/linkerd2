import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import _ from 'lodash';
import classNames from 'classnames';
import { getSuccessRateClassification } from './MetricUtils.jsx';
import { metricToFormatter } from './Utils.js';
import { statusClassNames } from './theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => statusClassNames(theme);

class SuccessRateMiniChart extends React.Component {
  render() {
    const { sr, classes } = this.props;

    return (
      <Grid container justify="flex-end" alignItems="center" spacing={8}>
        <Grid item>{metricToFormatter["SUCCESS_RATE"](sr)}</Grid>
        <Grid item>{_.isNil(sr) ? null :
        <div className={classNames("success-rate-dot", classes[getSuccessRateClassification(sr)])} />}
        </Grid>
      </Grid>
    );
  }
}

SuccessRateMiniChart.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  sr: PropTypes.number,
};

SuccessRateMiniChart.defaultProps = {
  sr: null
};

export default withStyles(styles, { withTheme: true })(SuccessRateMiniChart);
