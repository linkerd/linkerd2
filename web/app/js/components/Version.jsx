import { apiErrorPropType } from './util/ApiHelpers.jsx';
import Button from '@material-ui/core/Button';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';

class Version extends React.Component {
  static defaultProps = {
    error: null,
    latestVersion: '',
    productName: 'controller'
  }

  static propTypes = {
    error: apiErrorPropType,
    isLatest: PropTypes.bool.isRequired,
    latestVersion: PropTypes.string,
    productName: PropTypes.string,
    releaseVersion: PropTypes.string.isRequired,
  }

  numericVersion = version => {
    let parts = version.split("-", 2);
    if (parts.length === 2) {
      return parts[1];
    } else {
      return version;
    }
  }

  versionChannel = version => {
    let parts = version.split("-", 2);
    if (parts.length === 2) {
      return parts[0];
    }
  }

  renderVersionCheck = () => {
    const {latestVersion, error, isLatest} = this.props;

    if (!latestVersion) {
      return (
        <div>
          Version check failed{error ? `: ${error.statusText}` : ''}.
        </div>
      );
    }

    if (isLatest) {
      return "Linkerd is up to date.";
    }

    return (
      <div>
        <div className="new-version-text">A new version ({this.numericVersion(latestVersion)}) is available.</div>
        <Button
          variant="contained"
          color="primary"
          target="_blank"
          href="https://versioncheck.linkerd.io/update">
          Update Now
        </Button>
      </div>
    );
  }

  render() {
    let channel = this.versionChannel(this.props.releaseVersion);
    let message = `Running ${this.props.productName || "controller"}`;
    message += ` ${this.numericVersion(this.props.releaseVersion)}`;
    if (channel) {
      message += ` (${channel})`;
    }
    message += ".";

    return (
      <div className="version">
        {message}<br />
        {this.renderVersionCheck()}
      </div>
    );
  }
}

export default withContext(Version);
