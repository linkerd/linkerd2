import PropTypes from 'prop-types';
import React from 'react';
import Step from '@material-ui/core/Step';
import StepContent from '@material-ui/core/StepContent';
import StepLabel from '@material-ui/core/StepLabel';
import Stepper from '@material-ui/core/Stepper';
import Typography from '@material-ui/core/Typography';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import { withStyles } from '@material-ui/core/styles';
import { withTranslation } from 'react-i18next';

const styles = theme => ({
  root: {
    width: '90%',
  },
  button: {
    marginRight: theme.spacing.unit,
  },
  instructions: {
    marginTop: theme.spacing.unit,
    marginBottom: theme.spacing.unit,
  },
});

function getSteps(numResources, resource, t) {
  return [
    { label: t("Controller successfully installed") },
    { label: numResources ?
      t("message2", { count: numResources, resource: resource }) :
      t("message1", { resource: resource })
    },
    { label: t("message3", { resource: resource }), content: incompleteMeshMessage() }
  ];
}

class CallToAction extends React.Component {
  render() {
    const { resource, numResources, t } = this.props;
    const steps = getSteps(numResources, resource, t);
    const lastStep = steps.length - 1; // hardcode the last step as the active step

    return (
      <React.Fragment>
        <Typography>{t("The service mesh was successfully installed!")}</Typography>
        <Stepper
          activeStep={lastStep}
          orientation="vertical">
          {
            steps.map((step, i) => {
              const props = {};
              props.completed = i < lastStep; // select the last step as the currently active one

              return (
                <Step key={step.label} {...props}>
                  <StepLabel>{step.label}</StepLabel>
                  <StepContent>{step.content}</StepContent>
                </Step>
              );
            })
          }
        </Stepper>
      </React.Fragment>
    );
  }
}

CallToAction.propTypes = {
  numResources: PropTypes.number,
  resource: PropTypes.string.isRequired,
};

CallToAction.defaultProps = {
  numResources: null
};

export default withTranslation(["callToAction"])(withStyles(styles)(CallToAction));
