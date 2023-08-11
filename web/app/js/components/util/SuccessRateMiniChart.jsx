import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import _isNil from 'lodash/isNil';
import SuccessRateDot from './SuccessRateDot.jsx';
import { metricToFormatter } from './Utils.js';

const SuccessRateMiniChart = function({ sr }) {
  return (
    <Grid container justifyContent="flex-end" alignItems="center" spacing={1}>
      <Grid item>{metricToFormatter.SUCCESS_RATE(sr)}</Grid>
      <Grid item>{_isNil(sr) ? null :
      <SuccessRateDot sr={sr} />
    }
      </Grid>
    </Grid>
  );
};

SuccessRateMiniChart.propTypes = {
  sr: PropTypes.number,
};

SuccessRateMiniChart.defaultProps = {
  sr: null,
};

export default SuccessRateMiniChart;
