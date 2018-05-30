import { Link } from 'react-router-dom';
import React from 'react';
import './../../css/version.css';

export default class Version extends React.Component {
  renderVersionCheck() {
    if (!this.props.latest) {
      return (
        <div>
          Version check failed
          {this.props.error ? ": "+this.props.error : null}
        </div>
      );
    } else if (this.props.isLatest) {
      return "Conduit is up to date";
    } else {
      return (
        <div>
          A new version ({this.props.latest}) is available<br />
          <Link to="https://versioncheck.conduit.io/update" className="button primary" target="_blank">Update Now</Link>
        </div>
      );
    }
  }

  render() {
    return (
      <div className="version">
        Running Conduit {this.props.releaseVersion}<br />
        {this.renderVersionCheck()}
      </div>
    );
  }

}
