import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { Button, Form, Icon, Input } from 'antd';
import './../../css/tap.css';

class Tap extends React.Component {
  static propTypes = {
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      errors: "",
      pollingInterval: 2000,
      resource: "",
      namespace: "",
      messages: [],
      maxLinesToDisplay: 40,
      websocketRequestSent: false,
      tapUnderway: false
    };
  }

  componentWillUnmount() {
    this.stopTapPolling();
  }

  onWebsocketRecv = e => {
    this.setState({
      messages: _(this.state.messages)
        .push(e.data)
        .takeRight(this.state.maxLinesToDisplay)
        .value()
    });
  }

  onWebsocketClose = e => {
    if (!e.wasClean) {
      this.setState({
        errors: `Websocket [${e.code}] ${e.reason}`
      });
    }
    this.stopTapPolling();
  }

  startTapPolling() {
    this.setState({
      messages: [],
      tapUnderway: true
    });

    let tapWebSocket = `ws://${window.location.host}${this.props.pathPrefix}/api/tap`;
    this.ws = new WebSocket(tapWebSocket);
    this.ws.onmessage = this.onWebsocketRecv;
    this.ws.onclose = this.onWebsocketClose;

    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopTapPolling() {
    this.setState({
      websocketRequestSent: false,
      tapUnderway: false
    });

    this.ws.close();
    window.clearInterval(this.timerId);
  }

  loadFromServer = () => {
    if (this.state.websocketRequestSent) {
      return;
    }

    if (this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({
        id: "tap-web",
        resource: this.state.resource,
        namespace: this.state.namespace
      }));
      this.setState({ websocketRequestSent: true });
    }
  }

  handleTapStart = e => {
    e.preventDefault();
    this.startTapPolling();
  }

  handleTapStop = () => {
    this.stopTapPolling();
  }

  handleFormChange = formVal => {
    let state = {};
    return e => {
      state[formVal] = e.target.value;
      this.setState(state);
    };
  }

  renderTapForm = () => {
    return (
      <Form layout="inline">
        <Form.Item>
          <Input placeholder="Resource" onChange={this.handleFormChange("resource")} />
        </Form.Item>

        <Form.Item>
          <Input placeholder="Namespace" onChange={this.handleFormChange("namespace")} />
        </Form.Item>

        <Form.Item>
          { this.state.tapUnderway ?
            <Button type="primary" className="tap-stop" onClick={this.handleTapStop} disabled={false}>Stop</Button> :
            <Button type="primary" className="tap-start" onClick={this.handleTapStart} disabled={false}>Start</Button>
          }
        </Form.Item>
      </Form>
    );
  }

  render() {
    return (
      <div>
        {!this.state.errors ? null : <ErrorBanner message={this.state.errors} />}

        <PageHeader header="Tap" />
        {this.renderTapForm()}

        <div className="tap-display">
          <code>
            { this.state.tapUnderway && _.size(this.state.messages) === 0 ?
              <div><Icon type="loading" /> Starting tap on {this.state.resource} in namespace {this.state.namespace}</div> : null }
            { _.map(this.state.messages, (m, i) => <div key={`message-${i}`}>{m}</div>)}
          </code>
        </div>
      </div>
    );
  }
}

export default withContext(Tap);
