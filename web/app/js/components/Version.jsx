import { ApiHelpers } from './util/ApiHelpers.jsx';
import { Link } from 'react-router-dom';
import React from 'react';
import './../../css/version.css';

export default class Version extends React.Component {

  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);
    this.state = {
      error: null,
      latest: null,
      loaded: false,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.loadFromServer();
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let versionUrl = `https://versioncheck.conduit.io/version.json?version=${this.props.releaseVersion}?uuid=${this.props.uuid}`;
    let versionFetch = ApiHelpers("").fetch(versionUrl);
    // expose serverPromise for testing
    this.serverPromise = Promise.all([versionFetch])
      .then(([resp]) => {
        this.setState({
          latest: resp.version,
          loaded: true,
          pendingRequests: false,
        });
      }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      loaded: true,
      pendingRequests: false,
      error: e
    });
  }

  renderVersionCheck() {
    if (!this.state.loaded || this.state.pendingRequests) {
      return "Performing version check...";
    }

    if (!this.state.latest) {
      return (<div>
        Version check failed
        {this.state.error ? ": "+this.state.error : null}
      </div>);
    } else if (this.state.latest === this.props.releaseVersion) {
      return "Conduit is up to date";
    } else {
      return (<div>
        A new version ({this.state.latest}) is available<br />
        <Link to="https://versioncheck.conduit.io/update" className="button primary" target="_blank">Update Now</Link>
      </div>);
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
