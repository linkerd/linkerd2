import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    marginTop: theme.spacing.unit * 3,
    marginBottom: theme.spacing.unit * 3,
  },
});
class ConfigureProfilesMsg extends React.Component {
  render() {
    const { classes } = this.props;
    return (
      <Card className={classes.root}>
        <CardContent>
          <Typography>
            No traffic found.  Does the service have a service profile?  You can create one with the `linkerd profile` command.
          </Typography>
        </CardContent>
      </Card>
    );
  }
}

ConfigureProfilesMsg.propTypes = {
  classes: PropTypes.shape({}).isRequired
};

export default withStyles(styles, { withTheme: true })(ConfigureProfilesMsg);
