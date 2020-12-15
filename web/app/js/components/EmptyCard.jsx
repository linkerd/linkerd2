import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  card: {
    textAlign: 'center',
    paddingTop: '8px',
  },
});

const EmptyCard = ({ classes }) => {
  return (
    <Card className={classes.card} elevation={3}>
      <CardContent>
        <Typography>
          <Trans>NoDataToDisplayMsg</Trans>
        </Typography>
      </CardContent>
    </Card>
  );
};

export default withStyles(styles)(EmptyCard);
