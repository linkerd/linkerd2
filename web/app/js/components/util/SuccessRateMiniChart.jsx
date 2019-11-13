import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateDot from "./SuccessRateDot.jsx";
import _isNil from 'lodash/isNil';
import { metricToFormatter } from './Utils.js';

class SuccessRateMiniChart extends React.Component {
  render() {
    const { sr } = this.props;

    return (
      <Grid container justify="flex-end" alignItems="center" spacing={1}>
        <Grid item>{metricToFormatter["SUCCESS_RATE"](sr)}</Grid>
        <Grid item>{_isNil(sr) ? null :
        <SuccessRateDot sr={sr} />
        }
        </Grid>
      </Grid>
    );
  }
}

SuccessRateMiniChart.propTypes = {
  sr: PropTypes.number,
};

SuccessRateMiniChart.defaultProps = {
  sr: null
};

export default SuccessRateMiniChart;
