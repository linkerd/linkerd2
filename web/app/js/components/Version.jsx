import Button from '@material-ui/core/Button';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  version: {
    maxWidth: '250px',
    padding: theme.spacing(3),
  },
  versionMsg: {
    fontSize: '12px',
  },
  updateBtn: {
    marginTop: theme.spacing(1),
  },
});
class Version extends React.Component {
  numericVersion = version => {
    const parts = version.split('-', 2);
    if (parts.length === 2) {
      return parts[1];
    } else {
      return version;
    }
  }

  versionChannel = version => {
    const parts = version.split('-', 2);
    return parts.length === 2 ? parts[0] : null;
  }

  renderVersionCheck = () => {
    const { classes, latestVersion, error, isLatest } = this.props;

    if (!latestVersion) {
      return (
        <Typography className={classes.versionMsg}>
          <Trans>Version check failed{error ? `: ${error.statusText}` : ''}.</Trans>
        </Typography>
      );
    }

    if (isLatest) {
      return <Typography className={classes.versionMsg}><Trans>LinkerdIsUpToDateMsg</Trans></Typography>;
    }

    const versionText = this.numericVersion(latestVersion);

    return (
      <div>
        <Typography className={classes.versionMsg}>
          <Trans>
            A new version ({versionText}) is available.
          </Trans>
        </Typography>
        <Button
          className={classes.updateBtn}
          variant="contained"
          color="primary"
          target="_blank"
          href="https://versioncheck.linkerd.io/update">
          <Trans>Update Now</Trans>
        </Button>
      </div>
    );
  }

  render() {
    const { classes, releaseVersion, productName } = this.props;
    const channel = this.versionChannel(releaseVersion);
    let message = ` ${productName || 'controller'}`;
    message += ` ${this.numericVersion(releaseVersion)}`;
    if (channel) {
      message += ` (${channel})`;
    }
    message += '.';

    return (
      <div className={classes.version}>
        <Typography className={classes.versionMsg}><Trans>Running</Trans>{message}</Typography>
        {this.renderVersionCheck()}
      </div>
    );
  }
}

Version.propTypes = {
  error: apiErrorPropType,
  isLatest: PropTypes.bool.isRequired,
  latestVersion: PropTypes.string,
  productName: PropTypes.string,
  releaseVersion: PropTypes.string.isRequired,
};

Version.defaultProps = {
  error: null,
  latestVersion: '',
  productName: 'controller',
};

export default withStyles(styles, { withTheme: true })(withContext(Version));
