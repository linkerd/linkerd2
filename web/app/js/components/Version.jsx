import Button from '@material-ui/core/Button';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import { withTranslation } from 'react-i18next';

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
    const { classes, latestVersion, error, isLatest, t } = this.props;

    if (!latestVersion) {
      return (
        <Typography className={classes.versionMsg}>
          {t('Version check failed{error}',
            { error: error ? `: ${error.statusText}` : '' })}
        </Typography>
      );
    }

    if (isLatest) {
      return (
        <Typography className={classes.versionMsg}>
          {t('Linkerd is up to date.')}
        </Typography>
      );
    }

    return (
      <div>
        <Typography className={classes.versionMsg}>
          {t('A new version {latestVersion} is available', { latestVersion: this.numericVersion(latestVersion) })}
        </Typography>
        <Button
          className={classes.updateBtn}
          variant="contained"
          color="primary"
          target="_blank"
          href="https://versioncheck.linkerd.io/update">
          {t('Update Now')}
        </Button>
      </div>
    );
  }

  render() {
    const { classes, releaseVersion, productName, t } = this.props;
    const channel = this.versionChannel(releaseVersion);
    let message = t('Running {name}', { name: productName || 'controller' });
    message += ` ${this.numericVersion(releaseVersion)}`;
    if (channel) {
      message += ` (${channel})`;
    }
    message += '.';

    return (
      <div className={classes.version}>
        <Typography className={classes.versionMsg}>{message}</Typography>
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
  t: PropTypes.func.isRequired,
};

Version.defaultProps = {
  error: null,
  latestVersion: '',
  productName: 'controller',
};

export default withTranslation(['Shared'])(withStyles(styles, { withTheme: true })(withContext(Version)));
