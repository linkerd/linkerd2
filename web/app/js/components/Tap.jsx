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

    this.state = {
      errors: "",
      resource: "",
      namespace: "",
      messages: [],
      maxLinesToDisplay: 40,
      webSocketRequestSent: false
    };
  }

  componentWillUnmount() {
    this.ws.close(1000);
    this.stopTapStreaming();
    this.stopServerPolling();
  }

  onWebsocketOpen = () => {
    this.ws.send(JSON.stringify({
      id: "tap-web",
      resource: this.state.query.resource,
      namespace: this.state.query.namespace,
    }));
    this.setState({
      webSocketRequestSent: true,
      errors: ""
    });
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
    this.stopTapStreaming();
  }

  onWebsocketOpen = () => {
    this.ws.send(JSON.stringify({
      id: "tap-web",
      resource: this.state.resource,
      namespace: this.state.namespace
    }));
    this.setState({
      webSocketRequestSent: true
    });
  }

  startTapSteaming() {
    this.setState({
      messages: []
    });

    let protocol = window.location.protocol === "https:" ? "wss" : "ws";
    let tapWebSocket = `${protocol}://${window.location.host}${this.props.pathPrefix}/api/tap`;

    this.ws = new WebSocket(tapWebSocket);
    this.ws.onmessage = this.onWebsocketRecv;
    this.ws.onclose = this.onWebsocketClose;
    this.ws.onopen = this.onWebsocketOpen;
  }

  stopTapStreaming() {
    this.setState({
      webSocketRequestSent: false
    });

    window.clearInterval(this.timerId);
  }

  handleTapStart = e => {
    e.preventDefault();
    this.startTapSteaming();
  }

  handleTapStop = () => {
    this.ws.close(1000);
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
          { this.state.webSocketRequestSent ?
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
            { this.state.webSocketRequestSent && _.size(this.state.messages) === 0 ?
              <div><Icon type="loading" /> Starting tap on {this.state.resource} in namespace {this.state.namespace}</div> : null }
            { _.map(this.state.messages, (m, i) => <div key={`message-${i}`}>{m}</div>)}
          </code>
        </div>
      </div>
    );
  }
}

export default withContext(Tap);
