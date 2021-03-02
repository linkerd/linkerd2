import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Warning from '@material-ui/icons/Warning';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  cardContainer: {
    marginTop: theme.spacing(1.5),
  },
  container: {
    margin: theme.spacing(1),
  },
  iconWarning: {
    color: theme.status.dark.warning,
  },
});

const TapEnabledWarning = ({ resource, cardComponent, namespace, classes }) => {
  const component = (
    <Grid className={classes.container} container spacing={1} alignItems="center">
      <Grid item><Warning className={classes.iconWarning} /></Grid>
      <Grid item>
        <Trans>Pods under the resource {resource} in the {namespace} namespace are missing tap configurations (restart these pods to enable tap)</Trans>
      </Grid>
    </Grid>
  );

  if (cardComponent) {
    return (
      <Card className={classes.cardContainer}>
        <CardContent>{component}</CardContent>
      </Card>
    );
  } else {
    return component;
  }
};

TapEnabledWarning.propTypes = {
  resource: PropTypes.string.isRequired,
  namespace: PropTypes.string.isRequired,
  cardComponent: PropTypes.bool,
};

TapEnabledWarning.defaultProps = {
  cardComponent: false,
};

export default withStyles(styles)(TapEnabledWarning);
